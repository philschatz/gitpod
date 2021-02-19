package scheduler

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	wsk8s "github.com/gitpod-io/gitpod/common-go/kubernetes"

	corev1 "k8s.io/api/core/v1"
	res "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	fakek8s "k8s.io/client-go/kubernetes/fake"
)

var (
	testBaseTime  = time.Date(2020, 01, 01, 01, 01, 0, 0, time.UTC)
	testNamespace = "default"
)

type SchedulingResultKind string

const (
	resultBound              SchedulingResultKind = "bound"
	resultUnbound            SchedulingResultKind = "unbound"
	resultNotEnoughResources SchedulingResultKind = "notEnoughResources"
)

type SchedulingResult struct {
	Kind         SchedulingResultKind
	DeletedGhost string
}
type Expectation = map[string]SchedulingResult

func TestSchedulePod(t *testing.T) {
	tests := []struct {
		Desc         string
		Nodes        []*corev1.Node
		AssignedPods []*corev1.Pod
		QueuedPods   []*corev1.Pod
		Expectations []Expectation
	}{
		{
			Desc: "schedule all pods in one cycle",
			Nodes: []*corev1.Node{
				createTNode("node1", "10000Mi"),
			},
			AssignedPods: []*corev1.Pod{
				createWorkspacePod("ws0", "2000Mi", "node1", corev1.PodRunning, "100s"),
			},
			QueuedPods: []*corev1.Pod{
				createWorkspacePod("ws1", "2000Mi", "", corev1.PodPending, "10s"),
				createWorkspacePod("ws2", "2000Mi", "", corev1.PodPending, "8s"),
			},
			Expectations: []Expectation{
				{
					"ws1": {
						Kind: resultBound,
					},
					"ws2": {
						Kind: resultBound,
					},
				},
			},
		},
		{
			Desc: "schedule two pods in two cycles",
			Nodes: []*corev1.Node{
				createTNode("node1", "11000Mi"),
			},
			AssignedPods: []*corev1.Pod{
				createWorkspacePod("ws0", "2000Mi", "node1", corev1.PodRunning, "100s"),
				createGhostPod("ghost1", "2000Mi", "node1", corev1.PodRunning, "101s"),
				createGhostPod("ghost2", "2000Mi", "node1", corev1.PodRunning, "102s"),
				createGhostPod("ghost3", "2000Mi", "node1", corev1.PodRunning, "103s"),
			},
			QueuedPods: []*corev1.Pod{
				createWorkspacePod("ws1", "2000Mi", "", corev1.PodPending, "10s"),
				createWorkspacePod("ws2", "2000Mi", "", corev1.PodPending, "8s"),
			},
			Expectations: []Expectation{
				{
					"ws1": {
						Kind: resultBound,
					},
					"ws2": {
						Kind:         resultUnbound,
						DeletedGhost: "ghost3",
					},
				},
				{
					"ws2": {
						Kind: resultBound,
					},
				},
			},
		},
		{
			Desc: "replace exactly all ghosts on node",
			Nodes: []*corev1.Node{
				createTNode("node1", "11000Mi"),
			},
			AssignedPods: []*corev1.Pod{
				createGhostPod("ghost1", "2000Mi", "node1", corev1.PodRunning, "101s"),
				createGhostPod("ghost2", "2000Mi", "node1", corev1.PodRunning, "102s"),
				createGhostPod("ghost3", "2000Mi", "node1", corev1.PodRunning, "103s"),
				createGhostPod("ghost4", "2000Mi", "node1", corev1.PodRunning, "104s"),
				createGhostPod("ghost5", "2000Mi", "node1", corev1.PodRunning, "105s"),
			},
			QueuedPods: []*corev1.Pod{
				createWorkspacePod("ws1", "2000Mi", "", corev1.PodPending, "11s"),
				createWorkspacePod("ws2", "2000Mi", "", corev1.PodPending, "12s"),
				createWorkspacePod("ws3", "2000Mi", "", corev1.PodPending, "13s"),
				createWorkspacePod("ws4", "2000Mi", "", corev1.PodPending, "14s"),
				createWorkspacePod("ws5", "2000Mi", "", corev1.PodPending, "15s"),
			},
			Expectations: []Expectation{
				{
					"ws5": {
						Kind:         resultUnbound,
						DeletedGhost: "ghost5",
					},
					"ws4": {
						Kind:         resultUnbound,
						DeletedGhost: "ghost4",
					},
					"ws3": {
						Kind:         resultUnbound,
						DeletedGhost: "ghost3",
					},
					"ws2": {
						Kind:         resultUnbound,
						DeletedGhost: "ghost2",
					},
					"ws1": {
						Kind:         resultUnbound,
						DeletedGhost: "ghost1",
					},
				},
			},
		},
		// {
		// 	Desc: "replace all ghosts, but still not enough space",
		// 	Nodes: []*corev1.Node{
		// 		createTNode("node1", "11000Mi"),
		// 	},
		// 	AssignedPods: []*corev1.Pod{
		// 		createWorkspacePod("ws0", "2000Mi", "node1", corev1.PodRunning, "100s"),
		// 		createGhostPod("ghost1", "2000Mi", "node1", corev1.PodRunning, "101s"),
		// 		createGhostPod("ghost2", "2000Mi", "node1", corev1.PodRunning, "102s"),
		// 		createGhostPod("ghost3", "2000Mi", "node1", corev1.PodRunning, "103s"),
		// 		createGhostPod("ghost4", "2000Mi", "node1", corev1.PodRunning, "104s"),
		// 	},
		// 	QueuedPods: []*corev1.Pod{
		// 		createWorkspacePod("ws1", "2000Mi", "", corev1.PodPending, "11s"),
		// 		createWorkspacePod("ws2", "2000Mi", "", corev1.PodPending, "12s"),
		// 		createWorkspacePod("ws3", "2000Mi", "", corev1.PodPending, "13s"),
		// 		createWorkspacePod("ws4", "2000Mi", "", corev1.PodPending, "14s"),
		// 		createWorkspacePod("ws5", "2000Mi", "", corev1.PodPending, "15s"),
		// 	},
		// 	Expectations: []map[string]SchedulingResult{
		// 		{
		// 			"ws5": {
		// 				Kind:         resultUnbound,
		// 				DeletedGhost: "ghost4",
		// 			},
		// 			"ws4": {
		// 				Kind:         resultUnbound,
		// 				DeletedGhost: "ghost3",
		// 			},
		// 			"ws3": {
		// 				Kind:         resultUnbound,
		// 				DeletedGhost: "ghost2",
		// 			},
		// 			"ws2": {
		// 				Kind:         resultUnbound,
		// 				DeletedGhost: "ghost1",
		// 			},
		// 			"ws1": {
		// 				Kind: resultNotEnoughResources,
		// 			},
		// 		},
		// 		{
		// 			"ws5": {
		// 				Kind: resultBound,
		// 			},
		// 			"ws4": {
		// 				Kind: resultBound,
		// 			},
		// 			"ws3": {
		// 				Kind: resultBound,
		// 			},
		// 			"ws2": {
		// 				Kind: resultBound,
		// 			},
		// 			"ws1": {
		// 				Kind: resultNotEnoughResources,
		// 			},
		// 		},
		// 	},
		// },
	}
	for _, test := range tests {
		t.Run(test.Desc, func(t *testing.T) {
			// preparation
			var objs []runtime.Object
			for _, n := range test.Nodes {
				objs = append(objs, n)
			}
			for _, p := range test.AssignedPods {
				objs = append(objs, p)
			}
			for _, qp := range test.QueuedPods {
				objs = append(objs, qp)
			}

			client := fakek8s.NewSimpleClientset(objs...)
			scheduler, err := NewScheduler(Configuration{
				Namespace:     testNamespace,
				SchedulerName: "test-ws-scheduler",
				StrategyName:  "DensityAndExperience",
				DensityAndExperienceConfig: &DensityAndExperienceConfig{
					WorkspaceFreshPeriodSeconds: 120,
					NodeFreshWorkspaceLimit:     2,
				},
			}, client)
			if err != nil {
				t.Errorf("unexpected error: %+q", err)
				return
			}

			scheduler.strategy, err = CreateStrategy(scheduler.Config.StrategyName, scheduler.Config)
			if err != nil {
				t.Errorf("cannot create strategy: %w", err)
				return
			}

			ctx, cancel := context.WithCancel(context.Background())
			q := NewPriorityQueue(SortByPriority, queueInitialBackoff, queueMaximumBackoff)
			scheduler.queue = q
			scheduler.startInformer(ctx)

			bindPodToNode := func(ctx context.Context, pod *corev1.Pod, nodeName string, createEventFn CreateEventFunc) error {
				for _, p := range test.QueuedPods {
					if p.Name != pod.Name {
						continue
					}
					p.Spec.NodeName = nodeName
					return nil
				}
				return fmt.Errorf("could not find pod to bind: %s", pod.Name)
			}
			createEvent := func(ctx context.Context, namespace string, event *corev1.Event, opts metav1.CreateOptions) error {
				return nil
			}

			for _, qp := range test.QueuedPods {
				q.Add(qp)
			}

			for _, expectation := range test.Expectations {
				cycleCtx, cancelCycleCtx := context.WithTimeout(ctx, 10*time.Second)
				allGhostsGotDeleted, err := watchForGhostDeletions(cycleCtx, client, expectation)
				if err != nil {
					t.Fatal(err)
				}

				// actually run the test
				for _, exp := range expectation {
					pi, wasClosed := q.Pop()
					if wasClosed {
						t.Fatalf("queue was closed but still expected pods!")
					}

					err = scheduler.schedulePod(cycleCtx, pi, bindPodToNode, createEvent)
					if exp.Kind == resultNotEnoughResources {
						if err == nil {
							t.Fatalf("expected error for '%s' (kind: %s), got none!", pi.Pod.Name, exp.Kind)
						}
					} else {
						if err != nil {
							t.Fatal(err)
						}
					}
				}

				// compare result
				for podName, exp := range expectation {
					qp := findPod(podName, test.QueuedPods)
					if qp == nil {
						t.Fatalf("inconsistent test data: ")
					}

					if exp.Kind == resultBound {
						if qp.Spec.NodeName == "" {
							t.Fatalf("expected pod '%s' to be bound but it wasn't!", qp.Name)
						}
					} else if exp.Kind == resultUnbound {
						if qp.Spec.NodeName != "" {
							t.Fatalf("expected pod '%s' to be unbound but it was bound to '%s'!", qp.Name, qp.Spec.NodeName)
						}
					}
					if exp.DeletedGhost != "" {
						pod, err := client.CoreV1().Pods(testNamespace).Get(cycleCtx, exp.DeletedGhost, metav1.GetOptions{})
						if err == nil {
							t.Fatalf("expected ghost '%s' to be deleted for '%s' but was still present", exp.DeletedGhost, qp.Name)
						}
						if !strings.HasSuffix(err.Error(), "not found") && (pod == nil || pod.DeletionTimestamp != nil) {
							t.Fatal(err)
						}
					}
				}

				// make sure this round is "done" and we get a defined state for the next cycle
				q.MoveAllToActive("endCycle")
				<-allGhostsGotDeleted
				cancelCycleCtx()
			}

			// cleanup
			cancel()
			scheduler.queue.Close()
		})
	}
}

func watchForGhostDeletions(ctx context.Context, client *fakek8s.Clientset, expectation Expectation) (<-chan struct{}, error) {
	toDelete := map[string]bool{}
	for _, exp := range expectation {
		if exp.DeletedGhost != "" {
			toDelete[exp.DeletedGhost] = true
		}
	}

	w, err := client.CoreV1().Pods(testNamespace).Watch(ctx, metav1.ListOptions{
		LabelSelector: "workspaceType=ghost",
		Watch:         true,
	})
	if err != nil {
		return nil, err
	}

	allDeletedChan := make(chan struct{}, 1)
	go func() {
		defer close(allDeletedChan)
		defer w.Stop()

		for {
			select {
			case evt := <-w.ResultChan():
				if evt.Type == watch.Deleted {
					pod, ok := evt.Object.(*corev1.Pod)
					if !ok {
						panic("pod watcher received non-pod event - this should never happen")
					}

					delete(toDelete, pod.Name)
					if len(toDelete) == 0 {
						return
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return allDeletedChan, nil
}

func findPod(name string, queued []*corev1.Pod) *corev1.Pod {
	for _, q := range queued {
		if q.Name == name {
			return q
		}
	}
	return nil
}

func createTNode(name string, ram string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{
				corev1.ResourceMemory: res.MustParse(ram),
			},
		},
	}
}

func createWorkspacePod(name string, ram string, nodeName string, phase corev1.PodPhase, age string) *corev1.Pod {
	return createTPod(name, ram, nodeName, phase, age, map[string]string{
		"component":     "workspace",
		wsk8s.TypeLabel: "regular",
	})
}

func createGhostPod(name string, ram string, nodeName string, phase corev1.PodPhase, age string) *corev1.Pod {
	return createTPod(name, ram, nodeName, phase, age, map[string]string{
		"component":     "workspace",
		"headless":      "true",
		wsk8s.TypeLabel: "ghost",
	})
}

func createProbePod(name string, ram string, nodeName string, phase corev1.PodPhase, age string) *corev1.Pod {
	return createTPod(name, ram, nodeName, phase, age, map[string]string{
		"component":     "workspace",
		"headless":      "true",
		wsk8s.TypeLabel: "probe",
	})
}

func createTPod(name string, ram string, nodeName string, phase corev1.PodPhase, ageStr string, labels map[string]string) *corev1.Pod {
	creationTimestamp := testBaseTime.Add(-MustParseDuration(ageStr))
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         testNamespace,
			CreationTimestamp: metav1.NewTime(creationTimestamp),
			UID:               types.UID(name),
			Labels:            labels,
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
			Containers: []corev1.Container{
				{
					Name:  "workspace",
					Image: testWorkspaceImage,
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: res.MustParse(ram),
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: phase,
		},
	}
}

func MustParseDuration(str string) time.Duration {
	dur, err := time.ParseDuration(str)
	if err != nil {
		panic("duration does not parse")
	}
	return dur
}
