package klenstests

import (
	"context"
	"testing"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/k8s/kinds"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

// TestPodXRayFullTree exercises the full k9s-parity tree: containers (init
// + main + ephemeral), env / envFrom refs (CM present, Secret missing),
// ServiceAccount lookup, and a Volume node with CM / Secret / PVC + a
// Projected source.
func TestPodXRayFullTree(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "nginx", Namespace: "ns"},
		Spec: corev1.PodSpec{
			ServiceAccountName: "nginx-sa",
			InitContainers: []corev1.Container{{
				Name: "wait-for-db",
				EnvFrom: []corev1.EnvFromSource{{
					ConfigMapRef: &corev1.ConfigMapEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "db-config"},
					},
				}},
			}},
			Containers: []corev1.Container{{
				Name: "nginx",
				EnvFrom: []corev1.EnvFromSource{{
					ConfigMapRef: &corev1.ConfigMapEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "nginx-conf"},
					},
				}},
				Env: []corev1.EnvVar{{
					Name: "API_KEY",
					ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "api-key"},
					}},
				}},
			}},
			Volumes: []corev1.Volume{
				{Name: "cfg", VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "nginx-conf"},
					},
				}},
				{Name: "tls", VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{SecretName: "tls-cert"},
				}},
				{Name: "data", VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "cache-vol"},
				}},
				{Name: "proj", VolumeSource: corev1.VolumeSource{
					Projected: &corev1.ProjectedVolumeSource{Sources: []corev1.VolumeProjection{
						{ConfigMap: &corev1.ConfigMapProjection{
							LocalObjectReference: corev1.LocalObjectReference{Name: "shared-config"},
						}},
					}},
				}},
			},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	cs := fake.NewSimpleClientset(
		pod,
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "nginx-conf", Namespace: "ns"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "shared-config", Namespace: "ns"}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "tls-cert", Namespace: "ns"}},
		&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "cache-vol", Namespace: "ns"}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "nginx-sa", Namespace: "ns"}},
		// Note: db-config and api-key are intentionally missing from the cache.
	)

	k, ok := kinds.Default.Resolve("Pod")
	if !ok {
		t.Fatal("Pod not registered")
	}
	xrayer, ok := any(k).(kinds.XRayer)
	if !ok {
		t.Fatal("Pod does not implement XRayer")
	}
	tree, err := xrayer.XRay(kinds.Context{
		Ctx:    context.Background(),
		Lister: k8s.NewFakeLister(cs),
	}, "ns", "nginx")
	if err != nil {
		t.Fatalf("XRay: %v", err)
	}
	if tree == nil {
		t.Fatal("XRay returned nil tree")
	}
	if tree.Kind != "Pod" || tree.Name != "nginx" {
		t.Fatalf("root = %+v, want Pod/nginx", tree)
	}

	// Expected children order: init container, main container, SA, Volume.
	// (No ephemeral container in this fixture.)
	if got, want := len(tree.Children), 4; got != want {
		t.Fatalf("root children = %d, want %d (%+v)", got, want, childNames(tree))
	}

	// Init container with missing CM ref.
	init := tree.Children[0]
	if init.Kind != "Container" || init.Name != "wait-for-db (init)" {
		t.Errorf("init = %+v", init)
	}
	if len(init.Children) != 1 || init.Children[0].Kind != "ConfigMap" || init.Children[0].Status != "missing" {
		t.Errorf("init.Children = %+v, want [ConfigMap db-config (missing)]", init.Children)
	}

	// Main container with one present CM + one missing Secret.
	main := tree.Children[1]
	if main.Kind != "Container" || main.Name != "nginx" {
		t.Errorf("main = %+v", main)
	}
	cmFound, secMissing := false, false
	for _, ch := range main.Children {
		if ch.Kind == "ConfigMap" && ch.Name == "nginx-conf" && ch.Status == "" {
			cmFound = true
		}
		if ch.Kind == "Secret" && ch.Name == "api-key" && ch.Status == "missing" {
			secMissing = true
		}
	}
	if !cmFound || !secMissing {
		t.Errorf("main children = %+v", main.Children)
	}

	// ServiceAccount node (present).
	sa := tree.Children[2]
	if sa.Kind != "ServiceAccount" || sa.Name != "nginx-sa" || sa.Status != "" {
		t.Errorf("SA = %+v", sa)
	}

	// Volume parent contains CM/Secret/PVC + the Projected CM.
	vol := tree.Children[3]
	if vol.Kind != "Volume" {
		t.Errorf("vol kind = %q", vol.Kind)
	}
	if got, want := len(vol.Children), 4; got != want {
		t.Errorf("vol children = %d, want %d (%+v)", got, want, childNames(vol))
	}
}

// TestPodXRayContainerStatusBadges asserts container runtime state from
// pod.Status.{Init,Container}Statuses lands on the Container nodes — the
// mockup in spec 0003 shows `[Running]` / `[Completed]` badges on each
// container row.
func TestPodXRayContainerStatusBadges(t *testing.T) {
	pp := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "ns"},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{Name: "wait"}},
			Containers:     []corev1.Container{{Name: "app"}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			InitContainerStatuses: []corev1.ContainerStatus{{
				Name: "wait",
				State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
					Reason: "Completed",
				}},
			}},
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:  "app",
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
			}},
		},
	}
	cs := fake.NewSimpleClientset(pp)
	k, _ := kinds.Default.Resolve("Pod")
	tree, err := any(k).(kinds.XRayer).XRay(kinds.Context{
		Ctx:    context.Background(),
		Lister: k8s.NewFakeLister(cs),
	}, "ns", "demo")
	if err != nil {
		t.Fatalf("XRay: %v", err)
	}
	if len(tree.Children) < 2 {
		t.Fatalf("tree children = %d (%+v)", len(tree.Children), childNames(tree))
	}
	if got := tree.Children[0].Status; got != "Completed" {
		t.Errorf("init status = %q, want Completed", got)
	}
	if got := tree.Children[1].Status; got != "Running" {
		t.Errorf("main status = %q, want Running", got)
	}
}

// TestPodXRayMissingServiceAccount asserts that an SA name that isn't in
// the cache renders the SA node with Status="missing".
func TestPodXRayMissingServiceAccount(t *testing.T) {
	cs := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "ns"},
		Spec:       corev1.PodSpec{ServiceAccountName: "ghost"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	})
	k, _ := kinds.Default.Resolve("Pod")
	tree, err := any(k).(kinds.XRayer).XRay(kinds.Context{
		Ctx:    context.Background(),
		Lister: k8s.NewFakeLister(cs),
	}, "ns", "p")
	if err != nil {
		t.Fatalf("XRay: %v", err)
	}
	if tree == nil {
		t.Fatal("nil tree")
	}
	var sa *k8s.TreeNode
	for _, ch := range tree.Children {
		if ch.Kind == "ServiceAccount" {
			sa = ch
		}
	}
	if sa == nil {
		t.Fatal("no ServiceAccount child in tree")
	}
	if sa.Status != "missing" {
		t.Errorf("SA status = %q, want missing", sa.Status)
	}
}

// TestServiceAccountXRayMissingSecret covers the "why is image-pull failing"
// trail: a dangling ImagePullSecret renders with Status="missing".
func TestServiceAccountXRayMissingSecret(t *testing.T) {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "robot", Namespace: "ns"},
		Secrets: []corev1.ObjectReference{
			{Name: "robot-token"},
		},
		ImagePullSecrets: []corev1.LocalObjectReference{
			{Name: "registry-creds"},
		},
	}
	cs := fake.NewSimpleClientset(
		sa,
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "robot-token", Namespace: "ns"}},
		// registry-creds intentionally absent.
	)
	k, _ := kinds.Default.Resolve("ServiceAccount")
	tree, err := any(k).(kinds.XRayer).XRay(kinds.Context{
		Ctx:    context.Background(),
		Lister: k8s.NewFakeLister(cs),
	}, "ns", "robot")
	if err != nil {
		t.Fatalf("XRay: %v", err)
	}
	if tree == nil || len(tree.Children) != 2 {
		t.Fatalf("tree = %+v", tree)
	}
	if tree.Children[0].Kind != "Secret" || tree.Children[0].Status != "" {
		t.Errorf("secret = %+v", tree.Children[0])
	}
	if tree.Children[1].Kind != "ImagePullSecret" || tree.Children[1].Status != "missing" {
		t.Errorf("image pull secret = %+v", tree.Children[1])
	}
}

// TestStatefulSetXRayOwnerRefMatch confirms only owner-ref-matched pods are
// included; unrelated pods are excluded.
func TestStatefulSetXRayOwnerRefMatch(t *testing.T) {
	stsUID := types.UID("sts-uid")
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "redis", Namespace: "ns", UID: stsUID},
	}
	ownedPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "redis-0",
			Namespace:       "ns",
			OwnerReferences: []metav1.OwnerReference{{UID: stsUID}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	otherPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "stray", Namespace: "ns"},
	}
	cs := fake.NewSimpleClientset(sts, ownedPod, otherPod)
	k, _ := kinds.Default.Resolve("StatefulSet")
	tree, err := any(k).(kinds.XRayer).XRay(kinds.Context{
		Ctx:    context.Background(),
		Lister: k8s.NewFakeLister(cs),
	}, "ns", "redis")
	if err != nil {
		t.Fatalf("XRay: %v", err)
	}
	if tree == nil {
		t.Fatal("nil tree")
	}
	if len(tree.Children) != 1 {
		t.Fatalf("children = %d, want 1: %+v", len(tree.Children), childNames(tree))
	}
	if tree.Children[0].Name != "redis-0" {
		t.Errorf("pod child = %+v", tree.Children[0])
	}
}

// TestCronJobXRayTwoHops walks CronJob → Job → Pod via owner references.
func TestCronJobXRayTwoHops(t *testing.T) {
	cronUID := types.UID("cron-uid")
	jobUID := types.UID("job-uid")
	cron := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "nightly", Namespace: "ns", UID: cronUID},
	}
	jb := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "nightly-1",
			Namespace:       "ns",
			UID:             jobUID,
			OwnerReferences: []metav1.OwnerReference{{UID: cronUID}},
		},
	}
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "nightly-1-xyz",
			Namespace:       "ns",
			OwnerReferences: []metav1.OwnerReference{{UID: jobUID}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodSucceeded},
	}
	cs := fake.NewSimpleClientset(cron, jb, p)
	k, _ := kinds.Default.Resolve("CronJob")
	tree, err := any(k).(kinds.XRayer).XRay(kinds.Context{
		Ctx:    context.Background(),
		Lister: k8s.NewFakeLister(cs),
	}, "ns", "nightly")
	if err != nil {
		t.Fatalf("XRay: %v", err)
	}
	if tree == nil || len(tree.Children) != 1 {
		t.Fatalf("tree = %+v", tree)
	}
	jobNode := tree.Children[0]
	if jobNode.Kind != "Job" || jobNode.Name != "nightly-1" {
		t.Errorf("job = %+v", jobNode)
	}
	if len(jobNode.Children) != 1 || jobNode.Children[0].Kind != "Pod" {
		t.Errorf("job.Children = %+v", jobNode.Children)
	}
}

// childNames is a debug helper for failure messages.
func childNames(n *k8s.TreeNode) []string {
	out := make([]string, 0, len(n.Children))
	for _, c := range n.Children {
		out = append(out, c.Kind+"/"+c.Name)
	}
	return out
}
