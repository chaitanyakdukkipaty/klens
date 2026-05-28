package kinds

import (
	k8s "github.com/chaitanyak/klens/internal/k8s"
	corev1 "k8s.io/api/core/v1"
)

// XRay builds a k9s-parity tree for a Pod:
//
//	Pod
//	├─ Container (init) <name>           (env / envFrom CM+Secret refs)
//	├─ Container <name>                  (env / envFrom CM+Secret refs)
//	├─ Container (ephemeral) <name>      (env / envFrom CM+Secret refs)
//	├─ ServiceAccount <name>
//	└─ Volume
//	   ├─ ConfigMap <name>
//	   ├─ Secret    <name>
//	   └─ PVC       <name>
//
// References that aren't in the informer cache render with Status="missing".
func (p pod) XRay(c Context, ns, name string) (*k8s.TreeNode, error) {
	pods, err := listTyped[*corev1.Pod](c, p.Meta().GVR, ns)
	if err != nil {
		return nil, err
	}
	var pp *corev1.Pod
	for _, x := range pods {
		if x.Name == name {
			pp = x
			break
		}
	}
	if pp == nil {
		return nil, nil
	}

	// All four secondary informers are already started by the namespace's
	// WatcherFactory — the same lists describe already reads. Open cost ≈ 0
	// if synced; bounded LIST otherwise.
	cms, _ := listTyped[*corev1.ConfigMap](c, k8s.ConfigMapGVR, ns)
	secs, _ := listTyped[*corev1.Secret](c, k8s.SecretGVR, ns)
	pvcs, _ := listTyped[*corev1.PersistentVolumeClaim](c, k8s.PersistentVolumeClaimGVR, ns)
	sas, _ := listTyped[*corev1.ServiceAccount](c, k8s.ServiceAccountGVR, ns)

	root := &k8s.TreeNode{Kind: "Pod", Name: pp.Name, Status: podXRayStatus(pp)}

	// Container runtime state lives in pp.Status.{Init,Container,Ephemeral}Statuses,
	// keyed by container name. Each Container node renders its state
	// (Running / Completed / CrashLoopBackOff / …) as the status badge.
	initStatus := containerStatusByName(pp.Status.InitContainerStatuses)
	ctrStatus := containerStatusByName(pp.Status.ContainerStatuses)
	ephStatus := containerStatusByName(pp.Status.EphemeralContainerStatuses)

	for _, ctr := range pp.Spec.InitContainers {
		root.Children = append(root.Children, containerXRayNode(ctr, "init", initStatus[ctr.Name], cms, secs))
	}
	for _, ctr := range pp.Spec.Containers {
		root.Children = append(root.Children, containerXRayNode(ctr, "", ctrStatus[ctr.Name], cms, secs))
	}
	for _, ec := range pp.Spec.EphemeralContainers {
		// EphemeralContainerCommon is a copy of Container's spec fields, so
		// the conversion lets us reuse the same builder for env/envFrom refs.
		// The conversion fails to compile if the layouts ever diverge — the
		// desired signal.
		ctr := corev1.Container(ec.EphemeralContainerCommon)
		root.Children = append(root.Children, containerXRayNode(ctr, "ephemeral", ephStatus[ctr.Name], cms, secs))
	}

	if saName := pp.Spec.ServiceAccountName; saName != "" {
		root.Children = append(root.Children,
			lookupNode("ServiceAccount", saName, serviceAccountNames(sas)[saName]))
	}

	if vol := buildVolumeXRayNode(pp, cms, secs, pvcs); vol != nil {
		root.Children = append(root.Children, vol)
	}

	return root, nil
}
