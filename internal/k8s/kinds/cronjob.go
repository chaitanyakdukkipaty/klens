package kinds

import (
	"context"
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// cronJob implements only Deleter — the legacy descriptor had SupportsYAML,
// SupportsDeletion, no scale/logs/apply.
type cronJob struct{}

func (cronJob) Meta() Meta {
	return Meta{
		Kind:       "CronJob",
		Plural:     "cronjobs",
		Aliases:    []string{"cj"},
		Namespaced: true,
		GVR:        k8s.CronJobGVR,
	}
}

func (cronJob) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 40, Flex: true},
		{Header: "SCHEDULE", Width: 20},
		{Header: "LAST SCHEDULE", Width: 16},
		{Header: "AGE", Width: 10},
	}
}

func (j cronJob) List(c Context) ([]Row, error) {
	cjs, err := listTyped[*batchv1.CronJob](c, j.Meta().GVR, c.Namespace)
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(cjs))
	for _, cj := range cjs {
		lastSchedule := "Never"
		if cj.Status.LastScheduleTime != nil {
			lastSchedule = k8s.AgeString(*cj.Status.LastScheduleTime)
		}
		rows = append(rows, Row{
			Name:      cj.Name,
			Namespace: cj.Namespace,
			Values:    []string{cj.Name, cj.Spec.Schedule, lastSchedule, k8s.AgeString(cj.CreationTimestamp)},
			Raw:       cj,
		})
	}
	return rows, nil
}

func (cronJob) Fetch(ctx context.Context, c Context, ns, name string) (Object, error) {
	if c.Clientset == nil {
		return nil, fmt.Errorf("cronJob.Fetch: no Clientset")
	}
	return c.Clientset.BatchV1().CronJobs(ns).Get(ctx, name, metav1.GetOptions{})
}

func (cronJob) Delete(c Context, ns, name string) error {
	if c.Clientset == nil {
		return fmt.Errorf("cronJob.Delete: no Clientset")
	}
	grace := int64(0)
	return c.Clientset.BatchV1().CronJobs(ns).Delete(c.Ctx, name, metav1.DeleteOptions{GracePeriodSeconds: &grace})
}

func init() { register(cronJob{}) }
