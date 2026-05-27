package kinds

import (
	"context"
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// job implements Deleter + Logger.
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
		{Header: "NAME", Width: 40, Flex: true},
		{Header: "COMPLETIONS", Width: 14},
		{Header: "DURATION", Width: 12},
		{Header: "AGE", Width: 10},
	}
}

func (j job) List(c Context) ([]Row, error) {
	jobs, err := listTyped[*batchv1.Job](c, j.Meta().GVR, c.Namespace)
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(jobs))
	for _, jb := range jobs {
		completions := "0"
		if jb.Spec.Completions != nil {
			completions = fmt.Sprintf("%d/%d", jb.Status.Succeeded, *jb.Spec.Completions)
		}
		duration := ""
		if jb.Status.CompletionTime != nil && !jb.Status.StartTime.IsZero() {
			d := jb.Status.CompletionTime.Sub(jb.Status.StartTime.Time)
			duration = fmt.Sprintf("%.0fs", d.Seconds())
		}
		status := "Running"
		switch {
		case jb.Status.Succeeded > 0:
			status = "Succeeded"
		case jb.Status.Failed > 0:
			status = "Failed"
		}
		rows = append(rows, Row{
			Name:      jb.Name,
			Namespace: jb.Namespace,
			Status:    status,
			Values:    []string{jb.Name, completions, duration, k8s.AgeString(jb.CreationTimestamp)},
			Raw:       jb,
		})
	}
	return rows, nil
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

func init() { register(job{}) }
