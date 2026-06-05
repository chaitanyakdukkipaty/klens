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

// cronJob implements Deleter + XRayer. YAML view comes from the always-on
// Fetch; no scale/logs/apply.
type cronJob struct{}

func (cronJob) Meta() Meta {
	return Meta{
		Kind:       "CronJob",
		Group:      "Workloads",
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

// XRay walks CronJob → Job → Pod via owner refs on both hops. If a CronJob
// has zero active Jobs the tree has zero Job children — the truthful answer.
func (j cronJob) XRay(c Context, ns, name string) (*k8s.TreeNode, error) {
	crons, err := listTyped[*batchv1.CronJob](c, j.Meta().GVR, ns)
	if err != nil {
		return nil, err
	}
	var cj *batchv1.CronJob
	for _, x := range crons {
		if x.Name == name {
			cj = x
			break
		}
	}
	if cj == nil {
		return nil, nil
	}
	root := &k8s.TreeNode{Kind: "CronJob", Name: cj.Name}
	jobs, err := listTyped[*batchv1.Job](c, k8s.JobGVR, ns)
	if err != nil {
		return root, err
	}
	pods, err := listTyped[*corev1.Pod](c, k8s.PodGVR, ns)
	if err != nil {
		return root, err
	}
	for _, jb := range jobs {
		if !ownedByUID(jb.OwnerReferences, cj.UID) {
			continue
		}
		jobNode := &k8s.TreeNode{Kind: "Job", Name: jb.Name}
		for _, p := range pods {
			if ownedByUID(p.OwnerReferences, jb.UID) {
				jobNode.Children = append(jobNode.Children, podTreeNode(p))
			}
		}
		root.Children = append(root.Children, jobNode)
	}
	return root, nil
}

func init() { register(cronJob{}) }
