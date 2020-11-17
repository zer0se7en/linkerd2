package watcher

import (
	"sync"

	"github.com/linkerd/linkerd2/controller/k8s"
	logging "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
)

type (
	// PodWatcher TODO
	PodWatcher struct {
		k8sAPI     *k8s.API
		publishers map[string]*podSubscriptions
		log        *logging.Entry
		sync.RWMutex
	}

	podSubscriptions struct {
		ip        string
		pod       AddressSet
		listeners map[ProfileUpdateListener]Port
		log       *logging.Entry
		sync.Mutex
	}
)

// NewPodWatcher TODO
func NewPodWatcher(k8sAPI *k8s.API, log *logging.Entry) *PodWatcher {
	pw := &PodWatcher{
		k8sAPI:     k8sAPI,
		publishers: make(map[string]*podSubscriptions),
		log: log.WithFields(logging.Fields{
			"component": "pod-watcher",
		}),
	}

	k8sAPI.Pod().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: pw.deletePod,
	})

	return pw
}

// Subscribe TODO
func (pw *PodWatcher) Subscribe(ip string, port Port, listener ProfileUpdateListener) error {
	ps := pw.getOrNewPodSubscriptions(ip)
	return ps.subscribe(port, listener)
}

// Unsubscribe TODO
func (pw *PodWatcher) Unsubscribe(ip string, listener ProfileUpdateListener) {
	ps, ok := pw.getPodSubscriptions(ip)
	if !ok {
		pw.log.Errorf("Cannot unsubscribe from unknown pod ip [%s]", ip)
		return
	}
	ps.unsubscribe(listener)
}

func (pw *PodWatcher) getPodSubscriptions(ip string) (ps *podSubscriptions, ok bool) {
	pw.RLock()
	defer pw.RUnlock()
	ps, ok = pw.publishers[ip]
	return
}

func (pw *PodWatcher) getOrNewPodSubscriptions(ip string) *podSubscriptions {
	pw.Lock()
	defer pw.Unlock()

	// If the service doesn't yet exist, create a stub for it so the listener can
	// be registered.
	ps, ok := pw.publishers[ip]
	if !ok {
		ps = &podSubscriptions{
			ip:        ip,
			listeners: make(map[ProfileUpdateListener]Port),
			log:       pw.log.WithField("pod", ip),
		}

		objs, err := pw.k8sAPI.Pod().Informer().GetIndexer().ByIndex(podIPIndex, ip)
		if err != nil {
			pw.log.Error(err)
		} else {
			pods := []*corev1.Pod{}
			for _, obj := range objs {
				if pod, ok := obj.(*corev1.Pod); ok {
					// Skip pods with HostNetwork.
					if !pod.Spec.HostNetwork {
						pods = append(pods, pod)
					}
				}
			}
			if len(pods) > 1 {
				pw.log.Errorf("Pod IP conflict: %v, %v", objs[0], objs[1])
			}
			if len(pods) == 1 {
				pod := pods[0]
				ps.pod = PodToAddressSet(pw.k8sAPI, pod)
			}
		}

		pw.publishers[ip] = ps
	}
	return ps
}

func (pw *PodWatcher) deletePod(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			pw.log.Errorf("couldn't get object from DeletedFinalStateUnknown %#v", obj)
			return
		}
		pod, ok = tombstone.Obj.(*corev1.Pod)
		if !ok {
			pw.log.Errorf("DeletedFinalStateUnknown contained object that is not a Pod %#v", obj)
			return
		}
	}

	// TODO
	if pod.Namespace == kubeSystem {
		return
	}

	ps, ok := pw.getPodSubscriptions(pod.Status.PodIP)
	if ok {
		ps.deletePod()
	}
}

func (ps *podSubscriptions) deletePod() {
	ps.Lock()
	defer ps.Unlock()
	for listener := range ps.listeners {
		listener.DeleteEndpoint()
	}
	// TODO
	ps.pod = AddressSet{}
}

func (ps *podSubscriptions) subscribe(port Port, listener ProfileUpdateListener) error {
	ps.Lock()
	defer ps.Unlock()
	ps.listeners[listener] = port
	return nil
}

func (ps *podSubscriptions) unsubscribe(listener ProfileUpdateListener) {
	ps.Lock()
	defer ps.Unlock()
	delete(ps.listeners, listener)
}
