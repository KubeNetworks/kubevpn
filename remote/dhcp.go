package remote

import (
	"context"
	"encoding/json"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
)

var stopChan = make(chan os.Signal)

func addCleanUpResourceHandler(client *kubernetes.Clientset, namespace string, ip *net.IPNet) {
	signal.Notify(stopChan, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGKILL /*, syscall.SIGSTOP*/)
	go func() {
		<-stopChan
		log.Info("prepare to exit, cleaning up")
		cleanUpTrafficManagerIfRefCountIsZero(client, namespace)
		err := ReleaseIpToDHCP(client, namespace, ip)
		if err != nil {
			log.Errorf("failed to release ip to dhcp, err: %v", err)
		}
		log.Info("clean up successful")
		os.Exit(0)
	}()
}

func deletePod(client *kubernetes.Clientset, podName, namespace string, wait bool) {
	err := client.CoreV1().Pods(namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{})
	if !wait {
		return
	}
	if err != nil && errors.IsNotFound(err) {
		log.Info("not found shadow pod, no need to delete it")
		return
	}
	log.Infof("waiting for pod: %s to be deleted...", podName)
	if err == nil {
		w, errs := client.CoreV1().Pods(namespace).
			Watch(context.TODO(), metav1.ListOptions{
				FieldSelector: fields.OneTermEqualSelector("metadata.name", podName).String(),
				Watch:         true,
			})
		if errs != nil {
			log.Error(errs)
			return
		}
	out:
		for {
			select {
			case event := <-w.ResultChan():
				if watch.Deleted == event.Type {
					break out
				}
			}
		}
		log.Infof("delete pod: %s suecessfully", podName)
	}
}

// vendor/k8s.io/kubectl/pkg/polymorphichelpers/rollback.go:99
func updateRefCount(client *kubernetes.Clientset, namespace string, increment int) {
	err := retry.OnError(
		retry.DefaultRetry,
		func(err error) bool { return err != nil },
		func() error {
			configMap, err := client.CoreV1().ConfigMaps(namespace).Get(context.TODO(), TrafficManager, metav1.GetOptions{})
			if err != nil {
				log.Errorf("update ref-count failed, increment: %d, error: %v", increment, err)
				return err
			}
			curCount, err := strconv.Atoi(configMap.GetAnnotations()["ref-count"])
			if err != nil {
				curCount = 0
			}

			patch, _ := json.Marshal([]interface{}{
				map[string]interface{}{
					"op":    "replace",
					"path":  "/metadata/annotations/" + "ref-count",
					"value": strconv.Itoa(curCount + increment),
				},
			})
			_, err = client.CoreV1().ConfigMaps(namespace).
				Patch(context.TODO(), TrafficManager, types.JSONPatchType, patch, metav1.PatchOptions{})
			return err
		},
	)
	if err != nil {
		log.Errorf("update ref count error, error: %v", err)
	} else {
		log.Info("update ref count successfully")
	}
}

func cleanUpTrafficManagerIfRefCountIsZero(client *kubernetes.Clientset, namespace string) {
	updateRefCount(client, namespace, -1)
	configMap, err := client.CoreV1().ConfigMaps(namespace).Get(context.TODO(), TrafficManager, metav1.GetOptions{})
	if err != nil {
		log.Error(err)
		return
	}
	refCount, err := strconv.Atoi(configMap.GetAnnotations()["ref-count"])
	if err != nil {
		log.Error(err)
		return
	}
	// if refcount is less than zero or equals to zero, means no body will using this dns pod, so clean it
	if refCount <= 0 {
		log.Info("refCount is zero, prepare to clean up resource")
		_ = client.CoreV1().ConfigMaps(namespace).Delete(context.TODO(), TrafficManager, metav1.DeleteOptions{})
		_ = client.CoreV1().Pods(namespace).Delete(context.TODO(), TrafficManager, metav1.DeleteOptions{})
	}
}

func InitDHCP(client *kubernetes.Clientset, namespace string, addr *net.IPNet) error {
	get, err := client.CoreV1().ConfigMaps(namespace).Get(context.Background(), TrafficManager, metav1.GetOptions{})
	if err == nil && get != nil {
		return nil
	}
	if addr == nil {
		addr = &net.IPNet{IP: net.IPv4(196, 168, 254, 100), Mask: net.IPv4Mask(255, 255, 255, 0)}
	}
	var ips []string
	for i := 2; i < 254; i++ {
		if i != 100 {
			ips = append(ips, strconv.Itoa(i))
		}
	}
	result := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TrafficManager,
			Namespace: namespace,
			Labels:    map[string]string{},
		},
		Data: map[string]string{"DHCP": strings.Join(ips, ",")},
	}
	_, err = client.CoreV1().ConfigMaps(namespace).Create(context.Background(), result, metav1.CreateOptions{})
	if err != nil {
		log.Errorf("create dhcp error, err: %v", err)
		return err
	}
	return nil
}

func GetIpFromDHCP(client *kubernetes.Clientset, namespace string) (*net.IPNet, error) {
	get, err := client.CoreV1().ConfigMaps(namespace).Get(context.Background(), TrafficManager, metav1.GetOptions{})
	if err != nil {
		log.Errorf("failed to get ip from dhcp, err: %v", err)
		return nil, err
	}
	split := strings.Split(get.Data["DHCP"], ",")
	ip := split[0]
	split = split[1:]
	get.Data["DHCP"] = strings.Join(split, ",")
	_, err = client.CoreV1().ConfigMaps(namespace).Update(context.Background(), get, metav1.UpdateOptions{})
	if err != nil {
		log.Errorf("update dhcp error after get ip, need to put ip back, err: %v", err)
		return nil, err
	}
	atoi, _ := strconv.Atoi(ip)
	return &net.IPNet{
		IP:   net.IPv4(192, 168, 254, byte(atoi)),
		Mask: net.IPv4Mask(255, 255, 255, 0),
	}, nil
}

func ReleaseIpToDHCP(client *kubernetes.Clientset, namespace string, ip *net.IPNet) error {
	get, err := client.CoreV1().ConfigMaps(namespace).Get(context.Background(), TrafficManager, metav1.GetOptions{})
	if err != nil {
		log.Errorf("failed to get dhcp, err: %v", err)
		return err
	}
	split := strings.Split(get.Data["DHCP"], ",")
	split = append(split, strings.Split(ip.IP.To4().String(), ".")[3])
	get.Data["DHCP"] = strings.Join(split, ",")
	_, err = client.CoreV1().ConfigMaps(namespace).Update(context.Background(), get, metav1.UpdateOptions{})
	if err != nil {
		log.Errorf("update dhcp error after release ip, need to try again, err: %v", err)
		return err
	}
	return nil
}