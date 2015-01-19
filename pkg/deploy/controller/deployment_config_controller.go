package controller

import (
	kapi "github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api/errors"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/runtime"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	"github.com/golang/glog"
	deployapi "github.com/openshift/origin/pkg/deploy/api"
	deployutil "github.com/openshift/origin/pkg/deploy/util"
)

// DeploymentConfigController is responsible for creating a deployment when a DeploymentConfig is
// updated with a new LatestVersion. Any deployment created is correlated to a DeploymentConfig
// by setting the DeploymentConfigLabel on the deployment.
//
// Deployments are represented by ReplicationControllers. The DeploymentConfig used to create the
// ReplicationController is encoded and stored in an annotation on the ReplicationController.
type DeploymentConfigController struct {
	// DeploymentInterface provides access to Deployments.
	DeploymentInterface dccDeploymentInterface
	// NextDeploymentConfig blocks until the next DeploymentConfig is available.
	NextDeploymentConfig func() *deployapi.DeploymentConfig
	// Codec is used to encode DeploymentConfigs which are stored on deployments.
	Codec runtime.Codec
	// Stop is an optional channel that controls when the controller exits.
	Stop <-chan struct{}
}

// dccDeploymentInterface is a small private interface for dealing with Deployments.
type dccDeploymentInterface interface {
	GetDeployment(namespace, name string) (*kapi.ReplicationController, error)
	CreateDeployment(namespace string, deployment *kapi.ReplicationController) (*kapi.ReplicationController, error)
}

// Process DeploymentConfig events one at a time.
func (c *DeploymentConfigController) Run() {
	go util.Until(c.HandleDeploymentConfig, 0, c.Stop)
}

// Process a single DeploymentConfig event.
func (c *DeploymentConfigController) HandleDeploymentConfig() {
	config := c.NextDeploymentConfig()
	deploy, err := c.shouldDeploy(config)
	if err != nil {
		// TODO: better error handling
		glog.V(2).Infof("Error determining whether to redeploy deploymentConfig %v: %#v", config.Name, err)
		return
	}

	if !deploy {
		glog.V(4).Infof("Won't deploy from config %s", config.Name)
		return
	}

	if deployment, err := deployutil.MakeDeployment(config, c.Codec); err != nil {
		glog.V(2).Infof("Error making deployment from config %s: %v", config.Name, err)
	} else {
		glog.V(2).Infof("Creating new deployment from config %s", config.Name)
		if _, deployErr := c.DeploymentInterface.CreateDeployment(config.Namespace, deployment); deployErr != nil {
			glog.V(2).Infof("Error deploying config %s: %v", config.Name, deployErr)
		}
	}
}

// shouldDeploy returns true if the DeploymentConfig should have a new Deployment created.
func (c *DeploymentConfigController) shouldDeploy(config *deployapi.DeploymentConfig) (bool, error) {
	if config.LatestVersion == 0 {
		glog.V(4).Infof("Shouldn't deploy config %s with LatestVersion=0", config.Name)
		return false, nil
	}

	latestDeploymentID := deployutil.LatestDeploymentIDForConfig(config)
	deployment, err := c.DeploymentInterface.GetDeployment(config.Namespace, latestDeploymentID)

	if err != nil {
		if errors.IsNotFound(err) {
			glog.V(4).Infof("Should deploy config %s because there's no latest deployment", config.Name)
			return true, nil
		}
		glog.V(4).Infof("Shouldn't deploy config %s because of an error looking up latest deployment", config.Name)
		return false, err
	}

	glog.V(4).Infof("Shouldn't deploy because a deployment '%s' already exists for config %s", deployment.Name, config.Name)
	return false, nil
}
