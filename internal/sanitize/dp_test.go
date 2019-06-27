package sanitize

import (
	"context"
	"testing"

	"github.com/derailed/popeye/internal/cache"
	"github.com/derailed/popeye/internal/issues"
	"github.com/derailed/popeye/internal/k8s"
	"github.com/derailed/popeye/pkg/config"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	mv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
)

func TestDPSanitize(t *testing.T) {
	uu := map[string]struct {
		lister DeploymentLister
		issues issues.Issues
	}{
		"good": {
			lister: makeDPLister("d1", dpOpts{
				reps:      1,
				availReps: 1,
				coOpts: coOpts{
					image: "fred:0.0.1",
					rcpu:  "10m",
					rmem:  "10Mi",
					lcpu:  "10m",
					lmem:  "10Mi",
				},
				ccpu: "10m",
				cmem: "10Mi",
			}),
			issues: issues.Issues{},
		},
		"zeroReps": {
			lister: makeDPLister("d1", dpOpts{
				reps:      0,
				availReps: 1,
				coOpts: coOpts{
					image: "fred:0.0.1",
					rcpu:  "10m",
					rmem:  "10Mi",
					lcpu:  "10m",
					lmem:  "10Mi",
				},
				ccpu: "10m",
				cmem: "10Mi",
			}),
			issues: issues.Issues{
				issues.New(issues.Root, issues.WarnLevel, "Zero scale detected"),
			},
		},
		"noAvailReps": {
			lister: makeDPLister("d1", dpOpts{
				reps:       1,
				availReps:  0,
				collisions: 0,
				coOpts: coOpts{
					image: "fred:0.0.1",
					rcpu:  "10m",
					rmem:  "10Mi",
					lcpu:  "10m",
					lmem:  "10Mi",
				},
				ccpu: "10m",
				cmem: "10Mi",
			}),
			issues: issues.Issues{
				issues.New(issues.Root, issues.WarnLevel, "Used? No available replicas found"),
			},
		},
		"collisions": {
			lister: makeDPLister("d1", dpOpts{
				reps:       1,
				availReps:  1,
				collisions: 1,
				coOpts: coOpts{
					image: "fred:0.0.1",
					rcpu:  "10m",
					rmem:  "10Mi",
					lcpu:  "10m",
					lmem:  "10Mi",
				},
				ccpu: "10m",
				cmem: "10Mi",
			}),
			issues: issues.Issues{
				issues.New(issues.Root, issues.ErrorLevel, "ReplicaSet collisions detected (1)"),
			},
		},
	}

	for k, u := range uu {
		t.Run(k, func(t *testing.T) {
			dp := NewDeployment(issues.NewCollector(), u.lister)
			dp.Sanitize(context.Background())

			assert.Equal(t, u.issues, dp.Outcome()["default/d1"])
		})
	}
}

func TestDPSanitizeUtilization(t *testing.T) {
	uu := map[string]struct {
		lister DeploymentLister
		issues issues.Issues
	}{
		"bestEffort": {
			lister: makeDPLister("d1", dpOpts{
				reps:       2,
				availReps:  2,
				collisions: 0,
				coOpts: coOpts{
					image: "fred:0.0.1",
				},
				ccpu: "10m",
				cmem: "10Mi",
			}),
			issues: issues.Issues{
				issues.New("i1", issues.WarnLevel, "No resources defined"),
				issues.New("c1", issues.WarnLevel, "No resources defined"),
			},
		},
		"cpuUnderBurstable": {
			lister: makeDPLister("d1", dpOpts{
				reps:       2,
				availReps:  2,
				collisions: 0,
				coOpts: coOpts{
					image: "fred:0.0.1",
					rcpu:  "5m",
					rmem:  "10Mi",
					lcpu:  "10m",
					lmem:  "10Mi",
				},
				ccpu: "10m",
				cmem: "10Mi",
			}),
			issues: issues.Issues{
				issues.New(issues.Root, issues.WarnLevel, "At current load, CPU under allocated. Current:20m vs Requested:10m (200.00%)"),
			},
		},
		"cpuUnderGuaranteed": {
			lister: makeDPLister("d1", dpOpts{
				reps:       2,
				availReps:  2,
				collisions: 0,
				coOpts: coOpts{
					image: "fred:0.0.1",
					rcpu:  "5m",
					rmem:  "10Mi",
					lcpu:  "5m",
					lmem:  "10Mi",
				},
				ccpu: "10m",
				cmem: "10Mi",
			}),
			issues: issues.Issues{
				issues.New(issues.Root, issues.WarnLevel, "At current load, CPU under allocated. Current:20m vs Requested:10m (200.00%)"),
			},
		},
		// c=20 r=60 20/60=1/3 over is 50% req=3*c 33 > 100
		// c=60 r=20 60/20 3 under
		"cpuOverBustable": {
			lister: makeDPLister("d1", dpOpts{
				reps:       2,
				availReps:  2,
				collisions: 0,
				coOpts: coOpts{
					image: "fred:0.0.1",
					rcpu:  "30m",
					rmem:  "10Mi",
					lcpu:  "50m",
					lmem:  "10Mi",
				},
				ccpu: "10m",
				cmem: "10Mi",
			}),
			issues: issues.Issues{
				issues.New(issues.Root, issues.WarnLevel, "At current load, CPU over allocated. Current:20m vs Requested:60m (300.00%)"),
			},
		},
		"cpuOverGuaranteed": {
			lister: makeDPLister("d1", dpOpts{
				reps:       2,
				availReps:  2,
				collisions: 0,
				coOpts: coOpts{
					image: "fred:0.0.1",
					rcpu:  "30m",
					rmem:  "10Mi",
					lcpu:  "30m",
					lmem:  "10Mi",
				},
				ccpu: "10m",
				cmem: "10Mi",
			}),
			issues: issues.Issues{
				issues.New(issues.Root, issues.WarnLevel, "At current load, CPU over allocated. Current:20m vs Requested:60m (300.00%)"),
			},
		},
		"memUnderBurstable": {
			lister: makeDPLister("d1", dpOpts{
				reps:       2,
				availReps:  2,
				collisions: 0,
				coOpts: coOpts{
					image: "fred:0.0.1",
					rcpu:  "10m",
					rmem:  "5Mi",
					lcpu:  "20m",
					lmem:  "20Mi",
				},
				ccpu: "10m",
				cmem: "10Mi",
			}),
			issues: issues.Issues{
				issues.New(issues.Root, issues.WarnLevel, "At current load, Memory under allocated. Current:20Mi vs Requested:10Mi (200.00%)"),
			},
		},
		"memUnderGuaranteed": {
			lister: makeDPLister("d1", dpOpts{
				reps:       2,
				availReps:  2,
				collisions: 0,
				coOpts: coOpts{
					image: "fred:0.0.1",
					rcpu:  "10m",
					rmem:  "5Mi",
					lcpu:  "10m",
					lmem:  "5Mi",
				},
				ccpu: "10m",
				cmem: "10Mi",
			}),
			issues: issues.Issues{
				issues.New(issues.Root, issues.WarnLevel, "At current load, Memory under allocated. Current:20Mi vs Requested:10Mi (200.00%)"),
			},
		},
		"memOverBurstable": {
			lister: makeDPLister("d1", dpOpts{
				reps:       2,
				availReps:  2,
				collisions: 0,
				coOpts: coOpts{
					image: "fred:0.0.1",
					rcpu:  "10m",
					rmem:  "30Mi",
					lcpu:  "20m",
					lmem:  "60Mi",
				},
				ccpu: "10m",
				cmem: "10Mi",
			}),
			issues: issues.Issues{
				issues.New(issues.Root, issues.WarnLevel, "At current load, Memory over allocated. Current:20Mi vs Requested:60Mi (300.00%)"),
			},
		},
		"memOverGuaranteed": {
			lister: makeDPLister("d1", dpOpts{
				reps:       2,
				availReps:  2,
				collisions: 0,
				coOpts: coOpts{
					image: "fred:0.0.1",
					rcpu:  "10m",
					rmem:  "30Mi",
					lcpu:  "10m",
					lmem:  "30Mi",
				},
				ccpu: "10m",
				cmem: "10Mi",
			}),
			issues: issues.Issues{
				issues.New(issues.Root, issues.WarnLevel, "At current load, Memory over allocated. Current:20Mi vs Requested:60Mi (300.00%)"),
			},
		},
	}

	ctx := context.WithValue(context.Background(), PopeyeKey("OverAllocs"), true)
	for k, u := range uu {
		t.Run(k, func(t *testing.T) {
			dp := NewDeployment(issues.NewCollector(), u.lister)
			dp.Sanitize(ctx)

			assert.Equal(t, u.issues, dp.Outcome()["default/d1"])
		})
	}
}

// ----------------------------------------------------------------------------
// Helpers...

type (
	dpOpts struct {
		coOpts
		reps       int32
		availReps  int32
		collisions int32
		ccpu, cmem string
	}

	dp struct {
		name string
		opts dpOpts
	}
)

func makeDPLister(n string, opts dpOpts) *dp {
	return &dp{
		name: n,
		opts: opts,
	}
}

func (d *dp) CPUResourceLimits() config.Allocations {
	return config.Allocations{
		UnderPerc: 100,
		OverPerc:  50,
	}
}

func (d *dp) MEMResourceLimits() config.Allocations {
	return config.Allocations{
		UnderPerc: 100,
		OverPerc:  50,
	}
}

func (d *dp) ListPodsBySelector(sel *metav1.LabelSelector) map[string]*v1.Pod {
	return map[string]*v1.Pod{
		"default/p1": makeFullPod("p1", podOpts{
			coOpts: d.opts.coOpts,
		}),
	}
}

func (d *dp) RestartsLimit() int {
	return 10
}

func (d *dp) PodCPULimit() float64 {
	return 100
}

func (d *dp) PodMEMLimit() float64 {
	return 100
}

func (d *dp) ListPodsMetrics() map[string]*mv1beta1.PodMetrics {
	return map[string]*mv1beta1.PodMetrics{
		cache.FQN("default", "p1"): makeMxPod("p1", d.opts.ccpu, d.opts.cmem),
	}
}

func (d *dp) ListDeployments() map[string]*appsv1.Deployment {
	return map[string]*appsv1.Deployment{
		cache.FQN("default", d.name): makeDP(d.name, d.opts),
	}
}

func makeContainerMx(n, cpu, mem string) k8s.ContainerMetrics {
	return k8s.ContainerMetrics{
		n: k8s.Metrics{
			CurrentCPU: toQty(cpu),
			CurrentMEM: toQty(mem),
		},
	}
}

func makeDP(n string, o dpOpts) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      n,
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &o.reps,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"fred": "blee",
				},
			},
			Template: v1.PodTemplateSpec{
				Spec: v1.PodSpec{
					InitContainers: []v1.Container{
						makeContainer("i1", o.coOpts),
					},
					Containers: []v1.Container{
						makeContainer("c1", o.coOpts),
					},
				},
			},
		},
		Status: appsv1.DeploymentStatus{
			AvailableReplicas: o.availReps,
			CollisionCount:    &o.collisions,
		},
	}
}