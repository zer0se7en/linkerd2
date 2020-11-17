package watcher

import (
	"context"
	"fmt"

	"github.com/linkerd/linkerd2/controller/k8s"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

type (
	// ID is a namespace-qualified name.
	ID struct {
		Namespace string
		Name      string
	}
	// ServiceID is the namespace-qualified name of a service.
	ServiceID = ID
	// PodID is the namespace-qualified name of a pod.
	PodID = ID
	// ProfileID is the namespace-qualified name of a service profile.
	ProfileID = ID

	// Port is a numeric port.
	Port      = uint32
	namedPort = intstr.IntOrString

	// InvalidService is an error which indicates that the authority is not a
	// valid service.
	InvalidService struct {
		authority string
	}
)

func (is InvalidService) Error() string {
	return fmt.Sprintf("Invalid k8s service %s", is.authority)
}

func invalidService(authority string) InvalidService {
	return InvalidService{authority}
}

func (i ID) String() string {
	return fmt.Sprintf("%s/%s", i.Namespace, i.Name)
}

// PodToAddressSet converts a Pod spec into a set of addresses.
func PodToAddressSet(k8sAPI *k8s.API, pod *corev1.Pod) AddressSet {
	ownerKind, ownerName := k8sAPI.GetOwnerKindAndName(context.Background(), pod, true)
	return AddressSet{
		Addresses: map[PodID]Address{
			{
				Name:      pod.Name,
				Namespace: pod.Namespace,
			}: {
				IP:        pod.Status.PodIP,
				Port:      0, // Will be set by individual subscriptions
				Pod:       pod,
				OwnerName: ownerName,
				OwnerKind: ownerKind,
			},
		},
		Labels: map[string]string{"namespace": pod.Namespace},
	}
}
