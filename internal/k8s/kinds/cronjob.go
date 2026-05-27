package kinds

import (
	"context"
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
		{Header: "NAME", Width: 40, Flex: true, Render: cronJobName},
		{Header: "SCHEDULE", Width: 20, Render: cronJobSchedule},
		{Header: "LAST SCHEDULE", Width: 16, Render: cronJobLastSchedule},
		{Header: "AGE", Width: 10, Render: cronJobAge},
	}
}

func (j cronJob) List(c Context) ([]k8s.ResourceRow, error) { return listVia(j, c) }

func cronJobName(o runtime.Object, _ k8s.RowContext) string { return o.(*batchv1.CronJob).Name }

func cronJobSchedule(o runtime.Object, _ k8s.RowContext) string {
	return o.(*batchv1.CronJob).Spec.Schedule
}

func cronJobLastSchedule(o runtime.Object, _ k8s.RowContext) string {
	cj := o.(*batchv1.CronJob)
	if cj.Status.LastScheduleTime != nil {
		return k8s.AgeString(*cj.Status.LastScheduleTime)
	}
	return "Never"
}

func cronJobAge(o runtime.Object, _ k8s.RowContext) string {
	return k8s.AgeString(o.(*batchv1.CronJob).CreationTimestamp)
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
