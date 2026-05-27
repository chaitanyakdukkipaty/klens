package panels

import (
	"context"
	"encoding/json"

	tea "charm.land/bubbletea/v2"
	k8sres "github.com/chaitanyak/klens/internal/k8s"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// init keeps the last sliver of legacy registration: HelmRelease's
// suspend/resume actions. Every other kind has migrated to
// internal/k8s/kinds/ (plan 01) where capability presence is interface
// satisfaction. HelmRelease's suspend/resume go through the dynamic
// client; step 6 of plan 01 introduces a Suspender capability interface
// to absorb this last entry.
func init() {
	helmSuspendCmd := func(suspend bool) k8sres.Action {
		op := "resume"
		if suspend {
			op = "suspend"
		}
		return func(d k8sres.ActionDeps) tea.Cmd {
			return func() tea.Msg {
				ctx := context.Background()
				body, _ := json.Marshal(map[string]any{"spec": map[string]any{"suspend": suspend}})
				_, err := d.Dynamic.Resource(d.HelmGVR).Namespace(d.Namespace).Patch(
					ctx, d.Name, types.MergePatchType, body, metav1.PatchOptions{})
				if err != nil {
					return k8sres.OperationResultMsg{Operation: op, Resource: d.Name, Err: err}
				}
				return k8sres.OperationResultMsg{Operation: op, Resource: d.Name, Success: true}
			}
		}
	}
	k8sres.RegisterAction("HelmRelease", "suspend", helmSuspendCmd(true))
	k8sres.RegisterAction("HelmRelease", "resume", helmSuspendCmd(false))
}
