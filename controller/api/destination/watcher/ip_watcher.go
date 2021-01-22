package watcher

import (
	"context"
	"fmt"
	"sync"

	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
)

type (
	// IPWatcher wraps a EndpointsWatcher and allows subscriptions by
	// IP address.  It watches all services in the cluster to keep an index
	// of service by cluster IP and translates subscriptions by IP address into
	// subscriptions on the EndpointWatcher by service name.
	IPWatcher struct {
		publishers map[string]*serviceSubscriptions
		endpoints  *EndpointsWatcher
		k8sAPI     *k8s.API

		log          *logging.Entry
		sync.RWMutex // This mutex protects modification of the map itself.
	}

	serviceSubscriptions struct {
		clusterIP string

		// At most one of service or pod may be non-zero.
		service ServiceID
		pod     AddressSet

		listeners map[EndpointUpdateListener]Port
		endpoints *EndpointsWatcher

		log *logging.Entry
		// All access to the servicePublisher and its portPublishers is explicitly synchronized by
		// this mutex.
		sync.Mutex
	}
)

// WithPort sets the port field in all addresses of an address set.
func (as AddressSet) WithPort(port Port) AddressSet {
	wp := AddressSet{
		Addresses: map[PodID]Address{},
		Labels:    as.Labels,
	}
	for id, addr := range as.Addresses {
		addr.Port = port
		wp.Addresses[id] = addr
	}
	return wp
}

// NewIPWatcher creates an IPWatcher and begins watching the k8sAPI for service
// changes.
func NewIPWatcher(k8sAPI *k8s.API, endpoints *EndpointsWatcher, log *logging.Entry) *IPWatcher {
	iw := &IPWatcher{
		publishers: make(map[string]*serviceSubscriptions),
		endpoints:  endpoints,
		k8sAPI:     k8sAPI,
		log: log.WithFields(logging.Fields{
			"component": "ip-watcher",
		}),
	}

	k8sAPI.Svc().Informer().AddIndexers(cache.Indexers{podIPIndex: func(obj interface{}) ([]string, error) {
		if svc, ok := obj.(*corev1.Service); ok {
			return []string{svc.Spec.ClusterIP}, nil
		}
		return []string{""}, fmt.Errorf("object is not a service")
	}})

	k8sAPI.Svc().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    iw.addService,
		DeleteFunc: iw.deleteService,
		UpdateFunc: func(before interface{}, after interface{}) {
			iw.deleteService(before)
			iw.addService(after)
		},
	})

	k8sAPI.Pod().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    iw.addPod,
		DeleteFunc: iw.deletePod,
		UpdateFunc: func(_ interface{}, obj interface{}) {
			iw.addPod(obj)
		},
	})

	return iw
}

////////////////////////
/// IPWatcher ///
////////////////////////

// Subscribe to an authority.
// The provided listener will be updated each time the address set for the
// given authority is changed.
func (iw *IPWatcher) Subscribe(clusterIP string, port Port, listener EndpointUpdateListener) error {
	iw.log.Infof("Establishing watch on service cluster ip [%s:%d]", clusterIP, port)
	ss := iw.getOrNewServiceSubscriptions(clusterIP)
	return ss.subscribe(port, listener)
}

// Unsubscribe removes a listener from the subscribers list for this authority.
func (iw *IPWatcher) Unsubscribe(clusterIP string, port Port, listener EndpointUpdateListener) {
	iw.log.Infof("Stopping watch on service cluster ip [%s:%d]", clusterIP, port)
	ss, ok := iw.getServiceSubscriptions(clusterIP)
	if !ok {
		iw.log.Errorf("Cannot unsubscribe from unknown service ip [%s:%d]", clusterIP, port)
		return
	}
	ss.unsubscribe(port, listener)
}

// GetSvcID returns the service that corresponds to a Cluster IP address if one
// exists.
func (iw *IPWatcher) GetSvcID(clusterIP string) (*ServiceID, error) {
	resource, err := getResource(clusterIP, iw.k8sAPI.Svc().Informer())
	if err != nil {
		return nil, err
	}
	if svc, ok := resource.(*corev1.Service); ok {
		service := &ServiceID{
			Namespace: svc.Namespace,
			Name:      svc.Name,
		}
		return service, nil
	}
	// Either `clusterIP` does not map to a service that the indexer is aware
	// of, or it maps to a resource that is ignored
	return nil, nil
}

// GetPod returns the pod that corresponds to an IP address if one exists.
func (iw *IPWatcher) GetPod(podIP string) (*corev1.Pod, error) {
	resource, err := getResource(podIP, iw.k8sAPI.Pod().Informer())
	if err != nil {
		return nil, err
	}
	if pod, ok := resource.(*corev1.Pod); ok {
		return pod, nil
	}
	// Either `podIP` does not map to a pod that the indexer is aware of, or
	// it maps to a resource that is ignored
	return nil, nil
}

func getResource(ip string, informer cache.SharedIndexInformer) (interface{}, error) {
	matchingObjs, err := informer.GetIndexer().ByIndex(podIPIndex, ip)
	if err != nil {
		return nil, status.Error(codes.Unknown, err.Error())
	}

	objs := make([]interface{}, 0)
	for _, obj := range matchingObjs {
		// Ignore terminated pods,
		// their IPs can be reused for new Running pods
		pod, ok := obj.(*corev1.Pod)
		if ok && podTerminated(pod) {
			continue
		}

		objs = append(objs, obj)
	}

	if len(objs) > 1 {
		return nil, status.Errorf(codes.FailedPrecondition, "IP address conflict: %v, %v", objs[0], objs[1])
	}
	if len(objs) == 1 {
		return objs[0], nil
	}
	return nil, nil
}

func podTerminated(pod *corev1.Pod) bool {
	phase := pod.Status.Phase
	return phase == corev1.PodSucceeded || phase == corev1.PodFailed
}

func (iw *IPWatcher) addService(obj interface{}) {
	service := obj.(*corev1.Service)
	if service.Namespace == kubeSystem || service.Spec.ClusterIP == "None" {
		return
	}

	ss := iw.getOrNewServiceSubscriptions(service.Spec.ClusterIP)

	ss.updateService(service)
}

func (iw *IPWatcher) deleteService(obj interface{}) {
	service, ok := obj.(*corev1.Service)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			iw.log.Errorf("couldn't get object from DeletedFinalStateUnknown %#v", obj)
			return
		}
		service, ok = tombstone.Obj.(*corev1.Service)
		if !ok {
			iw.log.Errorf("DeletedFinalStateUnknown contained object that is not a Service %#v", obj)
			return
		}
	}

	if service.Namespace == kubeSystem {
		return
	}

	ss, ok := iw.getServiceSubscriptions(service.Spec.ClusterIP)
	if ok {
		ss.deleteService()
	}
}

func (iw *IPWatcher) addPod(obj interface{}) {
	pod := obj.(*corev1.Pod)
	if pod.Namespace == kubeSystem {
		return
	}
	if pod.Status.PodIP == "" {
		// Pod has not yet been assigned an IP address.
		return
	}
	if pod.Spec.HostNetwork {
		// Don't trigger updates for watches on host IP addresses.
		return
	}
	ss := iw.getOrNewServiceSubscriptions(pod.Status.PodIP)
	ss.updatePod(iw.PodToAddressSet(pod))
}

func (iw *IPWatcher) deletePod(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			iw.log.Errorf("couldn't get object from DeletedFinalStateUnknown %#v", obj)
			return
		}
		pod, ok = tombstone.Obj.(*corev1.Pod)
		if !ok {
			iw.log.Errorf("DeletedFinalStateUnknown contained object that is not a Pod %#v", obj)
			return
		}
	}

	if pod.Namespace == kubeSystem {
		return
	}

	ss, ok := iw.getServiceSubscriptions(pod.Status.PodIP)
	if ok {
		ss.deletePod()
	}
}

// Returns the serviceSubscriptions for the given clusterIP if it exists.  Otherwise,
// create a new one and return it.
func (iw *IPWatcher) getOrNewServiceSubscriptions(clusterIP string) *serviceSubscriptions {
	iw.Lock()
	defer iw.Unlock()

	// If the service doesn't yet exist, create a stub for it so the listener can
	// be registered.
	ss, ok := iw.publishers[clusterIP]
	if !ok {
		ss = &serviceSubscriptions{
			clusterIP: clusterIP,
			listeners: make(map[EndpointUpdateListener]Port),
			endpoints: iw.endpoints,
			log:       iw.log.WithField("clusterIP", clusterIP),
		}

		objs, err := iw.k8sAPI.Svc().Informer().GetIndexer().ByIndex(podIPIndex, clusterIP)
		if err != nil {
			iw.log.Error(err)
		} else {
			if len(objs) > 1 {
				iw.log.Errorf("Service cluster IP conflict: %v, %v", objs[0], objs[1])
			}
			if len(objs) == 1 {
				if svc, ok := objs[0].(*corev1.Service); ok {
					ss.service = ServiceID{
						Namespace: svc.Namespace,
						Name:      svc.Name,
					}
				}
			}
		}
		objs, err = iw.k8sAPI.Pod().Informer().GetIndexer().ByIndex(podIPIndex, clusterIP)
		if err != nil {
			iw.log.Error(err)
		} else {
			pods := []*corev1.Pod{}
			for _, obj := range objs {
				if pod, ok := obj.(*corev1.Pod); ok {
					// Skip pods with HostNetwork
					if pod.Spec.HostNetwork {
						continue
					}

					// Ignore terminated pods,
					// their IPs can be reused for new Running pods
					if podTerminated(pod) {
						continue
					}

					pods = append(pods, pod)
				}
			}
			if len(pods) > 1 {
				iw.log.Errorf("Pod IP conflict: %v, %v", objs[0], objs[1])
			}
			if len(pods) == 1 {
				ss.pod = iw.PodToAddressSet(pods[0])
			}
		}

		iw.publishers[clusterIP] = ss
	}
	return ss
}

func (iw *IPWatcher) getServiceSubscriptions(clusterIP string) (ss *serviceSubscriptions, ok bool) {
	iw.RLock()
	defer iw.RUnlock()
	ss, ok = iw.publishers[clusterIP]
	return
}

// PodToAddressSet converts a Pod spec into a set of Addresses.
func (iw *IPWatcher) PodToAddressSet(pod *corev1.Pod) AddressSet {
	ownerKind, ownerName := iw.k8sAPI.GetOwnerKindAndName(context.Background(), pod, true)
	return AddressSet{
		Addresses: map[PodID]Address{
			PodID{
				Name:      pod.Name,
				Namespace: pod.Namespace,
			}: Address{
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

////////////////////////
/// serviceSubscriptions ///
////////////////////////

func (ss *serviceSubscriptions) updateService(service *corev1.Service) {
	ss.Lock()
	defer ss.Unlock()

	id := ServiceID{
		Namespace: service.Namespace,
		Name:      service.Name,
	}

	if id != ss.service {
		for listener, port := range ss.listeners {
			ss.endpoints.Unsubscribe(ss.service, port, "", listener)
			listener.NoEndpoints(true) // Clear out previous endpoints.
			err := ss.endpoints.Subscribe(id, port, "", listener)
			if err != nil {
				ss.log.Warnf("failed to subscribe to %s: %s", id, err)
				listener.NoEndpoints(true) // Clear out previous endpoints.
				listener.Add(singletonAddress(ss.clusterIP, port))
			}
		}
		ss.service = id
		ss.pod = AddressSet{}
	}
}

func (ss *serviceSubscriptions) deleteService() {
	ss.Lock()
	defer ss.Unlock()

	for listener, port := range ss.listeners {
		ss.endpoints.Unsubscribe(ss.service, port, "", listener)
		listener.NoEndpoints(true) // Clear out previous endpoints.
		listener.Add(singletonAddress(ss.clusterIP, port))

	}
	ss.service = ServiceID{}
}

func (ss *serviceSubscriptions) updatePod(podSet AddressSet) {
	ss.Lock()
	defer ss.Unlock()

	for listener, port := range ss.listeners {
		// Pod IP addresses never change. Therefore, we don't need to send a
		// NoEndpoints update to clear our the previous address; sending an
		// Add with the same address will replace the previous one.
		if len(podSet.Addresses) != 0 {
			podSetWithPort := podSet.WithPort(port)
			listener.Add(podSetWithPort)
		}
	}
	ss.pod = podSet
}

func (ss *serviceSubscriptions) deletePod() {
	ss.Lock()
	defer ss.Unlock()
	for listener, port := range ss.listeners {
		listener.NoEndpoints(true) // Clear out previous endpoints.
		listener.Add(singletonAddress(ss.clusterIP, port))
	}
	ss.pod = AddressSet{}
}

func (ss *serviceSubscriptions) subscribe(port Port, listener EndpointUpdateListener) error {
	ss.Lock()
	defer ss.Unlock()

	if (ss.service != ServiceID{}) {
		err := ss.endpoints.Subscribe(ss.service, port, "", listener)
		if err != nil {
			return err
		}
	} else if len(ss.pod.Addresses) != 0 {
		podSetWithPort := ss.pod.WithPort(port)
		listener.Add(podSetWithPort)
	} else {
		listener.Add(singletonAddress(ss.clusterIP, port))
	}
	ss.listeners[listener] = port
	return nil
}

func (ss *serviceSubscriptions) unsubscribe(port Port, listener EndpointUpdateListener) {
	ss.Lock()
	defer ss.Unlock()

	if (ss.service != ServiceID{}) {
		ss.endpoints.Unsubscribe(ss.service, port, "", listener)
	}
	delete(ss.listeners, listener)
}

func singletonAddress(ip string, port Port) AddressSet {
	return AddressSet{
		Addresses: map[PodID]Address{
			PodID{}: Address{
				IP:   ip,
				Port: port,
			},
		},
		Labels: map[string]string{},
	}
}
