package kinds

import (
	"context"
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// job implements Deleter + Logger + XRayer.
type job struct{}

func (job) Meta() Meta {
	return Meta{
		Kind:       "Job",
		Plural:     "jobs",
		Aliases:    []string{"jo"},
		Namespaced: true,
		GVR:        k8s.JobGVR,
	}
}

func (job) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 40, Flex: true, Render: jobName},
		{Header: "COMPLETIONS", Width: 14, Render: jobCompletions},
		{Header: "DURATION", Width: 12, Render: jobDuration},
		{Header: "AGE", Width: 10, Render: jobAge},
	}
}

func (j job) List(c Context) ([]k8s.ResourceRow, error) { return listVia(j, c) }

func (job) RowStatus(o runtime.Object) string {
	jb := o.(*batchv1.Job)
	switch {
	case jb.Status.Succeeded > 0:
		return "Succeeded"
	case jb.Status.Failed > 0:
		return "Failed"
	default:
		return "Running"
	}
}

func jobName(o runtime.Object, _ k8s.RowContext) string { return o.(*batchv1.Job).Name }

func jobCompletions(o runtime.Object, _ k8s.RowContext) string {
	jb := o.(*batchv1.Job)
	if jb.Spec.Completions != nil {
		return fmt.Sprintf("%d/%d", jb.Status.Succeeded, *jb.Spec.Completions)
	}
	return "0"
}

func jobDuration(o runtime.Object, _ k8s.RowContext) string {
	jb := o.(*batchv1.Job)
	if jb.Status.CompletionTime != nil && !jb.Status.StartTime.IsZero() {
		d := jb.Status.CompletionTime.Sub(jb.Status.StartTime.Time)
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	return ""
}

func jobAge(o runtime.Object, _ k8s.RowContext) string {
	return k8s.AgeString(o.(*batchv1.Job).CreationTimestamp)
}

func (job) Fetch(ctx context.Context, c Context, ns, name string) (Object, error) {
	if c.Clientset == nil {
		return nil, fmt.Errorf("job.Fetch: no Clientset")
	}
	return c.Clientset.BatchV1().Jobs(ns).Get(ctx, name, metav1.GetOptions{})
}

func (job) Delete(c Context, ns, name string) error {
	if c.Clientset == nil {
		return fmt.Errorf("job.Delete: no Clientset")
	}
	grace := int64(0)
	return c.Clientset.BatchV1().Jobs(ns).Delete(c.Ctx, name, metav1.DeleteOptions{GracePeriodSeconds: &grace})
}

func (job) LogTargets(c Context, ns, name string) ([]LogTarget, error) {
	pods, err := listTyped[*corev1.Pod](c, k8s.PodGVR, ns)
	if err != nil {
		return nil, err
	}
	var out []LogTarget
	for _, p := range pods {
		for _, ref := range p.OwnerReferences {
			if ref.Kind == "Job" && ref.Name == name {
				out = append(out, LogTarget{Namespace: p.Namespace, Pod: p.Name})
				break
			}
		}
	}
	return out, nil
}

// XRay walks pods owned by this Job via owner reference UID.
func (j job) XRay(c Context, ns, name string) (*k8s.TreeNode, error) {
	jobs, err := listTyped[*batchv1.Job](c, j.Meta().GVR, ns)
	if err != nil {
		return nil, err
	}
	var jb *batchv1.Job
	for _, x := range jobs {
		if x.Name == name {
			jb = x
			break
		}
	}
	if jb == nil {
		return nil, nil
	}
	root := &k8s.TreeNode{Kind: "Job", Name: jb.Name}
	pods, err := listTyped[*corev1.Pod](c, k8s.PodGVR, ns)
	if err != nil {
		return root, err
	}
	for _, p := range pods {
		if ownedByUID(p.OwnerReferences, jb.UID) {
			root.Children = append(root.Children, podTreeNode(p))
		}
	}
	return root, nil
}

func init() { register(job{}) }
