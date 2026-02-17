package provingress

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Option configures an IngressProvisioner.
type Option func(*config) error

type config struct {
	namespace           string
	maxRoutesPerIngress int
	clientset           kubernetes.Interface
	managementLabel     string
	managementValue     string
}

// WithNamespace sets the Kubernetes namespace for Ingress resources.
func WithNamespace(ns string) Option {
	return func(c *config) error {
		c.namespace = ns
		return nil
	}
}

// WithMaxRoutesPerIngress sets the maximum number of IngressRules packed into
// a single Ingress resource. When an Ingress reaches capacity a new one is
// created. Default is 50.
func WithMaxRoutesPerIngress(max int) Option {
	return func(c *config) error {
		c.maxRoutesPerIngress = max
		return nil
	}
}

// WithKubeClient sets a pre-configured Kubernetes clientset.
func WithKubeClient(clientset kubernetes.Interface) Option {
	return func(c *config) error {
		c.clientset = clientset
		return nil
	}
}

// WithInClusterConfig creates a Kubernetes client using the in-cluster
// service account.
func WithInClusterConfig() Option {
	return func(c *config) error {
		cfg, err := rest.InClusterConfig()
		if err != nil {
			return err
		}
		cs, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			return err
		}
		c.clientset = cs
		return nil
	}
}

// WithKubeConfig creates a Kubernetes client from a kubeconfig file path.
func WithKubeConfig(path string) Option {
	return func(c *config) error {
		cfg, err := clientcmd.BuildConfigFromFlags("", path)
		if err != nil {
			return err
		}
		cs, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			return err
		}
		c.clientset = cs
		return nil
	}
}

// WithManagementLabel sets the label key and value used to identify Ingress
// resources managed by this provisioner. Defaults to
// app.kubernetes.io/managed-by=dynamic-route-provisioner.
func WithManagementLabel(key, value string) Option {
	return func(c *config) error {
		c.managementLabel = key
		c.managementValue = value
		return nil
	}
}
