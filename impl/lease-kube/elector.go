package leasekube

import (
	"context"
	"sync/atomic"

	"github.com/NicolasViaud/dynamic-route-provisioner/core/lease"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

// Compile-time check.
var _ lease.LeaderElector = (*KubeLeaderElector)(nil)

// KubeLeaderElector implements leader election using the Kubernetes
// coordination/v1 Lease API via client-go's leaderelection package.
type KubeLeaderElector struct {
	clientset kubernetes.Interface
	cfg       config
	leading   atomic.Bool
}

// New creates a KubeLeaderElector.
func New(clientset kubernetes.Interface, opts ...Option) *KubeLeaderElector {
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}

	return &KubeLeaderElector{
		clientset: clientset,
		cfg:       cfg,
	}
}

// Name returns "kube-lease".
func (e *KubeLeaderElector) Name() string { return "kube-lease" }

// IsLeader reports whether this instance currently holds the lease.
func (e *KubeLeaderElector) IsLeader() bool { return e.leading.Load() }

// Run starts the Kubernetes leader election loop. It blocks until ctx
// is cancelled.
func (e *KubeLeaderElector) Run(ctx context.Context, cb lease.LeaderCallbacks) error {
	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      e.cfg.leaseName,
			Namespace: e.cfg.namespace,
		},
		Client: e.clientset.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: e.cfg.identity,
		},
	}

	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock:            lock,
		LeaseDuration:   e.cfg.leaseDuration,
		RenewDeadline:   e.cfg.renewDeadline,
		RetryPeriod:     e.cfg.retryPeriod,
		ReleaseOnCancel: true,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				e.leading.Store(true)
				cb.OnStartedLeading(ctx)
			},
			OnStoppedLeading: func() {
				e.leading.Store(false)
				if cb.OnStoppedLeading != nil {
					cb.OnStoppedLeading()
				}
			},
		},
	})

	return nil
}
