package connect

import (
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/consul-helm/test/acceptance/framework"
	"github.com/hashicorp/consul-helm/test/acceptance/helpers"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
)

func TestSyncCatalog(t *testing.T) {
	cases := []struct {
		name       string
		helmValues map[string]string
	}{
		{
			"Default installation",
			map[string]string{
				"syncCatalog.enabled": "true",
			},
		},
		{
			"Secure installation (with TLS and ACLs enabled)",
			map[string]string{
				"syncCatalog.enabled":          "true",
				"global.tls.enabled":           "true",
				"global.acls.manageSystemACLs": "true",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := suite.Environment()

			helmValues := map[string]string{
				"syncCatalog.enabled": "true",
			}

			releaseName := helpers.RandomName()
			consulCluster := framework.NewHelmCluster(t, helmValues, env.DefaultContext(), releaseName)

			consulCluster.Create(t)

			createTestService(t, env.DefaultContext().KubernetesClient(t), releaseName)

			consulClient := consulCluster.SetupConsulClient(t, false)

			var services map[string][]string
			syncedServiceName := fmt.Sprintf("%s-%s", releaseName, env.DefaultContext().KubectlOptions().Namespace)
			counter := &retry.Counter{Count: 10, Wait: 5 * time.Second}
			retry.RunWith(counter, t, func(r *retry.R) {
				var err error
				services, _, err = consulClient.Catalog().Services(nil)
				require.NoError(r, err)
				if _, ok := services[syncedServiceName]; !ok {
					r.Errorf("service '%s' is not in Consul's list of services %s", syncedServiceName, services)
				}
			})
			service, _, err := consulClient.Catalog().Service(syncedServiceName, "", nil)
			require.NoError(t, err)
			require.Equal(t, 1, len(service))
			require.Equal(t, []string{"k8s"}, service[0].ServiceTags)
		})
	}

}

func createTestService(t *testing.T, k8sClient *kubernetes.Clientset, name string) {
	// Create a service in k8s and check that it exists in Consul
	svc, err := k8sClient.CoreV1().Services("default").Create(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeNodePort,
			Selector: map[string]string{"app": "test-pod"},
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080)},
			},
		},
	})
	require.NoError(t, err)

	pod, err := k8sClient.CoreV1().Pods("default").Create(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			// todo: create random name
			Name:   name,
			Labels: map[string]string{"app": "test-pod"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					// todo: create random name
					Name:  "test-container",
					Image: "hashicorp/http-echo:latest",
					Args: []string{
						`-text="hello world"`,
						`-listen=:8080`,
					},
					Ports: []corev1.ContainerPort{
						{
							Name:          "http",
							ContainerPort: 8080,
						},
					},
				},
			},
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		k8sClient.CoreV1().Services("default").Delete(svc.Name, nil)
		k8sClient.CoreV1().Pods("default").Delete(pod.Name, nil)
	})
}