package test

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/tls"
	"github.com/linkerd/linkerd2/testutil"
)

//////////////////////
///   TEST SETUP   ///
//////////////////////

var (
	TestHelper *testutil.TestHelper
)

func TestMain(m *testing.M) {
	TestHelper = testutil.NewTestHelper()
	os.Exit(m.Run())
}

var (
	configMapUID string

	helmTLSCerts *tls.CA

	linkerdSvcStable = []testutil.Service{
		{Namespace: "linkerd", Name: "linkerd-controller-api"},
		{Namespace: "linkerd", Name: "linkerd-dst"},
		{Namespace: "linkerd", Name: "linkerd-grafana"},
		{Namespace: "linkerd", Name: "linkerd-identity"},
		{Namespace: "linkerd", Name: "linkerd-prometheus"},
		{Namespace: "linkerd", Name: "linkerd-web"},
		{Namespace: "linkerd", Name: "linkerd-tap"},
		{Namespace: "linkerd", Name: "linkerd-dst-headless"},
		{Namespace: "linkerd", Name: "linkerd-identity-headless"},
	}

	linkerdSvcEdge = []testutil.Service{
		{Namespace: "linkerd", Name: "linkerd-controller-api"},
		{Namespace: "linkerd", Name: "linkerd-dst"},
		{Namespace: "linkerd-viz", Name: "linkerd-grafana"},
		{Namespace: "linkerd", Name: "linkerd-identity"},
		{Namespace: "linkerd-viz", Name: "linkerd-prometheus"},
		{Namespace: "linkerd-viz", Name: "linkerd-web"},
		{Namespace: "linkerd-viz", Name: "linkerd-tap"},
		{Namespace: "linkerd", Name: "linkerd-dst-headless"},
		{Namespace: "linkerd", Name: "linkerd-identity-headless"},
	}

	multiclusterSvcs = []testutil.Service{
		{Namespace: "linkerd-multicluster", Name: "linkerd-gateway"},
	}

	injectionCases = []struct {
		ns          string
		annotations map[string]string
		injectArgs  []string
	}{
		{
			ns: "smoke-test",
			annotations: map[string]string{
				k8s.ProxyInjectAnnotation: k8s.ProxyInjectEnabled,
			},
			injectArgs: nil,
		},
		{
			ns:         "smoke-test-manual",
			injectArgs: []string{"--manual"},
		},
		{
			ns:         "smoke-test-ann",
			injectArgs: []string{},
		},
	}

	//skippedInboundPorts lists some ports to be marked as skipped, which will
	// be verified in test/integration/inject
	skippedInboundPorts = "1234,5678"
)

//////////////////////
/// TEST EXECUTION ///
//////////////////////

// Tests are executed in serial in the order defined
// Later tests depend on the success of earlier tests

func TestVersionPreInstall(t *testing.T) {
	version := "unavailable"
	if TestHelper.UpgradeFromVersion() != "" {
		version = TestHelper.UpgradeFromVersion()
	}

	err := TestHelper.CheckVersion(version)
	if err != nil {
		testutil.AnnotatedFatalf(t, "Version command failed", "Version command failed\n%s", err.Error())
	}
}

func TestCheckPreInstall(t *testing.T) {
	if TestHelper.ExternalIssuer() {
		t.Skip("Skipping pre-install check for external issuer test")
	}

	if TestHelper.UpgradeFromVersion() != "" {
		t.Skip("Skipping pre-install check for upgrade test")
	}

	cmd := []string{"check", "--pre", "--expected-version", TestHelper.GetVersion()}
	golden := "check.pre.golden"
	out, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd check' command failed", err)
	}

	err = TestHelper.ValidateOutput(out, golden)
	if err != nil {
		testutil.AnnotatedFatalf(t, "received unexpected output", "received unexpected output\n%s", err.Error())
	}
}

func TestUpgradeTestAppWorksBeforeUpgrade(t *testing.T) {
	if TestHelper.UpgradeFromVersion() != "" {
		ctx := context.Background()
		// make sure app is running
		testAppNamespace := "upgrade-test"
		for _, deploy := range []string{"emoji", "voting", "web"} {
			if err := TestHelper.CheckPods(ctx, testAppNamespace, deploy, 1); err != nil {
				if rce, ok := err.(*testutil.RestartCountError); ok {
					testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
				} else {
					testutil.AnnotatedError(t, "CheckPods timed-out", err)
				}
			}

			if err := TestHelper.CheckDeployment(ctx, testAppNamespace, deploy, 1); err != nil {
				testutil.AnnotatedErrorf(t, "CheckDeployment timed-out", "Error validating deployment [%s]:\n%s", deploy, err)
			}
		}

		if err := testutil.ExerciseTestAppEndpoint("/api/list", testAppNamespace, TestHelper); err != nil {
			testutil.AnnotatedFatalf(t, "error exercising test app endpoint before upgrade",
				"error exercising test app endpoint before upgrade %s", err)
		}
	} else {
		t.Skip("Skipping for non upgrade test")
	}
}

func TestRetrieveUidPreUpgrade(t *testing.T) {
	if TestHelper.UpgradeFromVersion() != "" {
		var err error
		configMapUID, err = TestHelper.KubernetesHelper.GetConfigUID(context.Background(), TestHelper.GetLinkerdNamespace())
		if err != nil || configMapUID == "" {
			testutil.AnnotatedFatalf(t, "error retrieving linkerd-config's uid",
				"error retrieving linkerd-config's uid: %s", err)
		}
	}
}

func TestInstallCalico(t *testing.T) {
	if !TestHelper.Calico() {
		return
	}

	// Install calico CNI plug-in from the official manifests
	// Calico operator and custom resource definitions.
	out, err := TestHelper.Kubectl("", []string{"apply", "-f", "https://docs.projectcalico.org/manifests/tigera-operator.yaml"}...)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"kubectl apply command failed\n%s", out)
	}

	// wait for the tigera-operator deployment
	name := "tigera-operator"
	ns := "tigera-operator"
	o, err := TestHelper.Kubectl("", "--namespace="+ns, "wait", "--for=condition=available", "--timeout=120s", "deploy/"+name)
	if err != nil {
		testutil.AnnotatedFatalf(t, fmt.Sprintf("failed to wait for condition=available for deploy/%s in namespace %s", name, ns),
			"failed to wait for condition=available for deploy/%s in namespace %s: %s: %s", name, ns, err, o)
	}

	// creating the necessary custom resource
	out, err = TestHelper.Kubectl("", []string{"apply", "-f", "https://docs.projectcalico.org/manifests/custom-resources.yaml"}...)
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"kubectl apply command failed\n%s", out)
	}

	// Wait for Calico CNI Installation, which is created by the operator based on the custom resource applied above
	time.Sleep(10 * time.Second)
	ns = "calico-system"
	o, err = TestHelper.Kubectl("", "--namespace="+ns, "wait", "--for=condition=available", "--timeout=120s", "deploy/calico-kube-controllers", "deploy/calico-typha")
	if err != nil {
		testutil.AnnotatedFatalf(t, fmt.Sprintf("failed to wait for condition=available for resources in namespace %s", ns),
			"failed to wait for condition=available for resources in namespace %s: %s: %s", ns, err, o)
	}
}

func TestInstallCNIPlugin(t *testing.T) {
	if !TestHelper.CNI() {
		return
	}

	// install the CNI plugin in the cluster
	var (
		cmd  = "install-cni"
		args = []string{
			"--use-wait-flag",
			"--cni-log-level=debug",
			// For Flannel (k3d's default CNI) the following settings are required.
			// For Calico the default ones are fine.
			//"--dest-cni-net-dir=/var/lib/rancher/k3s/agent/etc/cni/net.d",
			//"--dest-cni-bin-dir=/bin",
		}
	)

	exec := append([]string{cmd}, args...)
	out, err := TestHelper.LinkerdRun(exec...)
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd install-cni' command failed", err)
	}

	out, err = TestHelper.KubectlApply(out, "")
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}

	// perform a linkerd check with --linkerd-cni-enabled
	timeout := time.Minute
	err = TestHelper.RetryFor(timeout, func() error {
		out, err = TestHelper.LinkerdRun("check", "--pre", "--linkerd-cni-enabled", "--wait=0")
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		testutil.AnnotatedFatal(t, fmt.Sprintf("'linkerd check' command timed-out (%s)", timeout), err)
	}
}

func TestInstallOrUpgradeCli(t *testing.T) {
	if TestHelper.GetHelmReleaseName() != "" {
		return
	}

	var (
		cmd  = "install"
		args = []string{
			"--controller-log-level", "debug",
			"--proxy-version", TestHelper.GetVersion(),
			"--skip-inbound-ports", skippedInboundPorts,
		}
		vizCmd  = []string{"viz", "install"}
		vizArgs = []string{
			"--set", fmt.Sprintf("namespace=%s", TestHelper.GetVizNamespace()),
		}
	)

	if certsPath := TestHelper.CertsPath(); certsPath != "" {
		args = append(args,
			"--identity-trust-anchors-file", certsPath+"/ca.crt",
			"--identity-issuer-certificate-file", certsPath+"/issuer.crt",
			"--identity-issuer-key-file", certsPath+"/issuer.key",
		)
	}

	if TestHelper.GetClusterDomain() != "cluster.local" {
		args = append(args, "--cluster-domain", TestHelper.GetClusterDomain())
		vizArgs = append(vizArgs, "--set", fmt.Sprintf("clusterDomain=%s", TestHelper.GetClusterDomain()))
	}

	if TestHelper.CNI() {
		args = append(args, "--linkerd-cni-enabled")
	}

	if TestHelper.ExternalIssuer() {

		// short cert lifetime to put some pressure on the CSR request, response code path
		args = append(args, "--identity-issuance-lifetime=15s", "--identity-external-issuer=true")

		err := TestHelper.CreateControlPlaneNamespaceIfNotExists(context.Background(), TestHelper.GetLinkerdNamespace())
		if err != nil {
			testutil.AnnotatedFatalf(t, fmt.Sprintf("failed to create %s namespace", TestHelper.GetLinkerdNamespace()),
				"failed to create %s namespace: %s", TestHelper.GetLinkerdNamespace(), err)
		}

		identity := fmt.Sprintf("identity.%s.%s", TestHelper.GetLinkerdNamespace(), TestHelper.GetClusterDomain())

		root, err := tls.GenerateRootCAWithDefaults(identity)
		if err != nil {
			testutil.AnnotatedFatal(t, "error generating root CA", err)
		}

		// instead of passing the roots and key around we generate
		// two secrets here. The second one will be used in the
		// external_issuer_test to update the first one and trigger
		// cert rotation in the identity service. That allows us
		// to generated the certs on the fly and use custom domain.

		if err = TestHelper.CreateTLSSecret(
			k8s.IdentityIssuerSecretName,
			root.Cred.Crt.EncodeCertificatePEM(),
			root.Cred.Crt.EncodeCertificatePEM(),
			root.Cred.EncodePrivateKeyPEM()); err != nil {
			testutil.AnnotatedFatal(t, "error creating TLS secret", err)
		}

		crt2, err := root.GenerateCA(identity, -1)
		if err != nil {
			testutil.AnnotatedFatal(t, "error generating CA", err)
		}

		if err = TestHelper.CreateTLSSecret(
			k8s.IdentityIssuerSecretName+"-new",
			root.Cred.Crt.EncodeCertificatePEM(),
			crt2.Cred.EncodeCertificatePEM(),
			crt2.Cred.EncodePrivateKeyPEM()); err != nil {
			testutil.AnnotatedFatal(t, "error creating TLS secret (-new)", err)
		}
	}

	if TestHelper.UpgradeFromVersion() != "" {

		cmd = "upgrade"
		// test 2-stage install during upgrade
		out, err := TestHelper.LinkerdRun(cmd, "config")
		if err != nil {
			testutil.AnnotatedFatal(t, "'linkerd upgrade config' command failed", err)
		}

		// apply stage 1
		out, err = TestHelper.KubectlApply(out, "")
		if err != nil {
			testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
				"kubectl apply command failed\n%s", out)
		}

		// prepare for stage 2
		args = append([]string{"control-plane", "--addon-overwrite"}, args...)
	}

	exec := append([]string{cmd}, args...)
	out, err := TestHelper.LinkerdRun(exec...)
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd install' command failed", err)
	}

	// test `linkerd upgrade --from-manifests`
	if TestHelper.UpgradeFromVersion() != "" {
		kubeArgs := append([]string{"--namespace", TestHelper.GetLinkerdNamespace(), "get"}, "configmaps", "-oyaml")
		configManifests, err := TestHelper.Kubectl("", kubeArgs...)
		if err != nil {
			testutil.AnnotatedFatalf(t, "'kubectl get' command failed",
				"'kubectl get' command failed with %s\n%s\n%s", err, configManifests, kubeArgs)
		}

		kubeArgs = append([]string{"--namespace", TestHelper.GetLinkerdNamespace(), "get"}, "secrets", "-oyaml")
		secretManifests, err := TestHelper.Kubectl("", kubeArgs...)
		if err != nil {
			testutil.AnnotatedFatalf(t, "'kubectl get' command failed",
				"'kubectl get' command failed with %s\n%s\n%s", err, secretManifests, kubeArgs)
		}

		manifests := configManifests + "---\n" + secretManifests

		exec = append(exec, "--from-manifests", "-")
		upgradeFromManifests, stderr, err := TestHelper.PipeToLinkerdRun(manifests, exec...)
		if err != nil {
			testutil.AnnotatedFatalf(t, "'linkerd upgrade --from-manifests' command failed",
				"'linkerd upgrade --from-manifests' command failed with %s\n%s\n%s\n%s", err, stderr, upgradeFromManifests, manifests)
		}

		if out != upgradeFromManifests {
			// retry in case it's just a discrepancy in the heartbeat cron schedule
			exec := append([]string{cmd}, args...)
			out, err := TestHelper.LinkerdRun(exec...)
			if err != nil {
				testutil.AnnotatedFatalf(t, fmt.Sprintf("command failed: %v", exec),
					"command failed: %v\n%s\n%s", exec, out, stderr)
			}

			if out != upgradeFromManifests {
				testutil.AnnotatedFatalf(t, "manifest upgrade differs from k8s upgrade",
					"manifest upgrade differs from k8s upgrade.\nk8s upgrade:\n%s\nmanifest upgrade:\n%s", out, upgradeFromManifests)
			}
		}
	}

	out, err = TestHelper.KubectlApply(out, "")
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}

	// Install Linkerd Viz Extension
	exec = append(vizCmd, vizArgs...)
	out, err = TestHelper.LinkerdRun(exec...)
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd viz install' command failed", err)
	}

	out, err = TestHelper.KubectlApply(out, "")
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}
}

// These need to be updated (if there are changes) once a new stable is released
func helmOverridesStable(root *tls.CA) []string {
	return []string{
		"--set", "global.controllerLogLevel=debug",
		"--set", "global.linkerdVersion=" + TestHelper.UpgradeHelmFromVersion(),
		"--set", "global.proxy.image.version=" + TestHelper.UpgradeHelmFromVersion(),
		"--set", "global.identityTrustDomain=cluster.local",
		"--set", "global.identityTrustAnchorsPEM=" + root.Cred.Crt.EncodeCertificatePEM(),
		"--set", "identity.issuer.tls.crtPEM=" + root.Cred.Crt.EncodeCertificatePEM(),
		"--set", "identity.issuer.tls.keyPEM=" + root.Cred.EncodePrivateKeyPEM(),
		"--set", "identity.issuer.crtExpiry=" + root.Cred.Crt.Certificate.NotAfter.Format(time.RFC3339),
	}
}

// These need to correspond to the flags in the current edge
func helmOverridesEdge(root *tls.CA) []string {
	skippedInboundPortsEscaped := strings.Replace(skippedInboundPorts, ",", "\\,", 1)
	return []string{
		"--set", "global.controllerLogLevel=debug",
		"--set", "global.linkerdVersion=" + TestHelper.GetVersion(),
		"--set", "global.proxy.image.version=" + TestHelper.GetVersion(),
		// these ports will get verified in test/integration/inject
		"--set", "global.proxyInit.ignoreInboundPorts=" + skippedInboundPortsEscaped,
		"--set", "global.identityTrustDomain=cluster.local",
		"--set", "global.identityTrustAnchorsPEM=" + root.Cred.Crt.EncodeCertificatePEM(),
		"--set", "identity.issuer.tls.crtPEM=" + root.Cred.Crt.EncodeCertificatePEM(),
		"--set", "identity.issuer.tls.keyPEM=" + root.Cred.EncodePrivateKeyPEM(),
		"--set", "identity.issuer.crtExpiry=" + root.Cred.Crt.Certificate.NotAfter.Format(time.RFC3339),
		"--set", "grafana.image.version=" + TestHelper.GetVersion(),
	}
}

func TestInstallHelm(t *testing.T) {
	if TestHelper.GetHelmReleaseName() == "" {
		return
	}

	cn := fmt.Sprintf("identity.%s.cluster.local", TestHelper.GetLinkerdNamespace())
	var err error
	helmTLSCerts, err = tls.GenerateRootCAWithDefaults(cn)
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to generate root certificate for identity",
			"failed to generate root certificate for identity: %s", err)
	}

	var chartToInstall string
	var args []string

	if TestHelper.UpgradeHelmFromVersion() != "" {
		chartToInstall = TestHelper.GetHelmStableChart()
		args = helmOverridesStable(helmTLSCerts)
	} else {
		chartToInstall = TestHelper.GetHelmChart()
		args = helmOverridesEdge(helmTLSCerts)
	}

	if stdout, stderr, err := TestHelper.HelmInstall(chartToInstall, args...); err != nil {
		testutil.AnnotatedFatalf(t, "'helm install' command failed",
			"'helm install' command failed\n%s\n%s", stdout, stderr)
	}

	// Wait for the proxy injector to be up
	name := "linkerd-proxy-injector"
	ns := "linkerd"
	o, err := TestHelper.Kubectl("", "--namespace="+ns, "wait", "--for=condition=available", "--timeout=120s", "deploy/"+name)
	if err != nil {
		testutil.AnnotatedFatalf(t, fmt.Sprintf("failed to wait for condition=available for deploy/%s in namespace %s", name, ns),
			"failed to wait for condition=available for deploy/%s in namespace %s: %s: %s", name, ns, err, o)
	}

	if TestHelper.UpgradeHelmFromVersion() == "" {
		vizChart := TestHelper.GetLinkerdVizHelmChart()
		vizArgs := []string{
			"--set", "linkerdVersion=" + TestHelper.GetVersion(),
			"--set", "namespace=" + TestHelper.GetVizNamespace(),
			"--set", "dashboard.image.tag=" + TestHelper.GetVersion(),
			"--set", "grafana.image.tag=" + TestHelper.GetVersion(),
			"--set", "tap.image.tag=" + TestHelper.GetVersion(),
			"--set", "metricsAPI.image.tag=" + TestHelper.GetVersion(),
			"--set", "tapInjector.image.tag=" + TestHelper.GetVersion(),
		}
		// Install Viz Extension Chart
		if stdout, stderr, err := TestHelper.HelmInstallPlain(vizChart, "l5d-viz", vizArgs...); err != nil {
			testutil.AnnotatedFatalf(t, "'helm install' command failed",
				"'helm install' command failed\n%s\n%s", stdout, stderr)
		}
	}
}

func TestControlPlaneResourcesPostInstall(t *testing.T) {
	expectedServices := linkerdSvcEdge
	expectedDeployments := testutil.LinkerdDeployReplicasEdge
	// Upgrade Case
	if TestHelper.UpgradeHelmFromVersion() != "" {
		expectedServices = linkerdSvcStable
		expectedDeployments = testutil.LinkerdDeployReplicasStable
	}
	testutil.TestResourcesPostInstall(TestHelper.GetLinkerdNamespace(), expectedServices, expectedDeployments, TestHelper, t)
}

func TestInstallMulticluster(t *testing.T) {
	if TestHelper.GetMulticlusterHelmReleaseName() != "" {
		flags := []string{
			"--set", "linkerdVersion=" + TestHelper.GetVersion(),
			"--set", "global.controllerImageVersion=" + TestHelper.GetVersion(),
		}
		if stdout, stderr, err := TestHelper.HelmInstallMulticluster(TestHelper.GetMulticlusterHelmChart(), flags...); err != nil {
			testutil.AnnotatedFatalf(t, "'helm install' command failed",
				"'helm install' command failed\n%s\n%s", stdout, stderr)
		}
	} else if TestHelper.Multicluster() {
		exec := append([]string{"multicluster"}, []string{
			"install",
			"--namespace", TestHelper.GetMulticlusterNamespace(),
		}...)
		out, err := TestHelper.LinkerdRun(exec...)
		if err != nil {
			testutil.AnnotatedFatal(t, "'linkerd multicluster install' command failed", err)
		}

		out, err = TestHelper.KubectlApply(out, "")
		if err != nil {
			testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
				"'kubectl apply' command failed\n%s", out)
		}
	}
}

func TestMulticlusterResourcesPostInstall(t *testing.T) {
	if !TestHelper.Multicluster() {
		return
	}
	testutil.TestResourcesPostInstall(TestHelper.GetMulticlusterNamespace(), multiclusterSvcs, testutil.MulticlusterDeployReplicas, TestHelper, t)
}

func TestCheckHelmStableBeforeUpgrade(t *testing.T) {
	if TestHelper.UpgradeHelmFromVersion() == "" {
		t.Skip("Skipping as this is not a helm upgrade test")
	}

	// TODO: make checkOutput as true once 2.9 releases
	testCheckCommand(t, "", TestHelper.UpgradeHelmFromVersion(), "", TestHelper.UpgradeHelmFromVersion(), false)
}

func TestUpgradeHelm(t *testing.T) {
	if TestHelper.UpgradeHelmFromVersion() == "" {
		t.Skip("Skipping as this is not a helm upgrade test")
	}

	args := []string{
		// implicit as at least one value is set manually: "--reset-values",
		// (see https://medium.com/@kcatstack/understand-helm-upgrade-flags-reset-values-reuse-values-6e58ac8f127e )

		// Also ensure that the CPU requests are fairly small (<100m) in order
		// to avoid squeeze-out of other pods in CI tests.

		"--set", "global.proxy.resources.cpu.limit=200m",
		"--set", "global.proxy.resources.cpu.request=20m",
		"--set", "global.proxy.resources.memory.limit=200Mi",
		"--set", "global.proxy.resources.memory.request=100Mi",
		// actually sets the value for the controller pod
		"--set", "publicAPIProxyResources.cpu.limit=1010m",
		"--set", "publicAPIProxyResources.memory.request=101Mi",
		"--set", "destinationProxyResources.cpu.limit=1020m",
		"--set", "destinationProxyResources.memory.request=102Mi",
		"--set", "identityProxyResources.cpu.limit=1040m",
		"--set", "identityProxyResources.memory.request=104Mi",
		"--set", "proxyInjectorProxyResources.cpu.limit=1060m",
		"--set", "proxyInjectorProxyResources.memory.request=106Mi",
		"--set", "spValidatorProxyResources.cpu.limit=1080m",
		"--set", "spValidatorProxyResources.memory.request=108Mi",
		"--atomic",
		"--wait",
	}
	args = append(args, helmOverridesEdge(helmTLSCerts)...)
	if stdout, stderr, err := TestHelper.HelmUpgrade(TestHelper.GetHelmChart(), args...); err != nil {
		testutil.AnnotatedFatalf(t, "'helm upgrade' command failed",
			"'helm upgrade' command failed\n%s\n%s", stdout, stderr)
	}

	// Install Viz Extension, as there was no viz with stable
	// TOODO: Update this to upgrade once this will be the newer stable/edge
	vizChart := TestHelper.GetLinkerdVizHelmChart()
	vizArgs := []string{
		"--set", "linkerdVersion=" + TestHelper.GetVersion(),
		"--set", "namespace=" + TestHelper.GetVizNamespace(),
		"--set", "dashboard.image.tag=" + TestHelper.GetVersion(),
		"--set", "grafana.image.tag=" + TestHelper.GetVersion(),
		"--set", "tap.image.tag=" + TestHelper.GetVersion(),
		"--set", "metricsAPI.image.tag=" + TestHelper.GetVersion(),
		"--set", "tapInjector.image.tag=" + TestHelper.GetVersion(),
		"--wait",
	}
	// Install Viz Extension Chart
	if stdout, stderr, err := TestHelper.HelmInstallPlain(vizChart, "l5d-viz", vizArgs...); err != nil {
		testutil.AnnotatedFatalf(t, "'helm install' command failed",
			"'helm install' command failed\n%s\n%s", stdout, stderr)
	}
}

func TestRetrieveUidPostUpgrade(t *testing.T) {
	if TestHelper.UpgradeFromVersion() != "" {
		newConfigMapUID, err := TestHelper.KubernetesHelper.GetConfigUID(context.Background(), TestHelper.GetLinkerdNamespace())
		if err != nil || newConfigMapUID == "" {
			testutil.AnnotatedFatalf(t, "error retrieving linkerd-config's uid",
				"error retrieving linkerd-config's uid: %s", err)
		}
		if configMapUID != newConfigMapUID {
			testutil.AnnotatedFatalf(t, "linkerd-config's uid after upgrade doesn't match its value before the upgrade",
				"linkerd-config's uid after upgrade [%s] doesn't match its value before the upgrade [%s]",
				newConfigMapUID, configMapUID,
			)
		}
	}
}

type expectedData struct {
	pod        string
	cpuLimit   string
	cpuRequest string
	memLimit   string
	memRequest string
}

var expectedResources = []expectedData{
	{
		pod:        "linkerd-controller",
		cpuLimit:   "1010m",
		cpuRequest: "20m",
		memLimit:   "200Mi",
		memRequest: "101Mi",
	},
	{
		pod:        "linkerd-destination",
		cpuLimit:   "1020m",
		cpuRequest: "20m",
		memLimit:   "200Mi",
		memRequest: "102Mi",
	},
	{
		pod:        "linkerd-identity",
		cpuLimit:   "1040m",
		cpuRequest: "20m",
		memLimit:   "200Mi",
		memRequest: "104Mi",
	},
	{
		pod:        "linkerd-proxy-injector",
		cpuLimit:   "1060m",
		cpuRequest: "20m",
		memLimit:   "200Mi",
		memRequest: "106Mi",
	},
	{
		pod:        "linkerd-sp-validator",
		cpuLimit:   "1080m",
		cpuRequest: "20m",
		memLimit:   "200Mi",
		memRequest: "108Mi",
	},
}

func TestComponentProxyResources(t *testing.T) {
	if TestHelper.UpgradeHelmFromVersion() == "" {
		t.Skip("Skipping as this is not a helm upgrade test")
	}

	for _, expected := range expectedResources {
		resourceReqs, err := TestHelper.GetResources(context.Background(), "linkerd-proxy", expected.pod, TestHelper.GetLinkerdNamespace())
		if err != nil {
			testutil.AnnotatedFatalf(t, "setting proxy resources failed", "Error retrieving resource requirements for %s: %s", expected.pod, err)
		}

		cpuLimitStr := resourceReqs.Limits.Cpu().String()
		if cpuLimitStr != expected.cpuLimit {
			testutil.AnnotatedFatalf(t, "setting proxy resources failed", "unexpected %s CPU limit: expected %s, was %s", expected.pod, expected.cpuLimit, cpuLimitStr)
		}
		cpuRequestStr := resourceReqs.Requests.Cpu().String()
		if cpuRequestStr != expected.cpuRequest {
			testutil.AnnotatedFatalf(t, "setting proxy resources failed", "unexpected %s CPU request: expected %s, was %s", expected.pod, expected.cpuRequest, cpuRequestStr)
		}
		memLimitStr := resourceReqs.Limits.Memory().String()
		if memLimitStr != expected.memLimit {
			testutil.AnnotatedFatalf(t, "setting proxy resources failed", "unexpected %s memory limit: expected %s, was %s", expected.pod, expected.memLimit, memLimitStr)
		}
		memRequestStr := resourceReqs.Requests.Memory().String()
		if memRequestStr != expected.memRequest {
			testutil.AnnotatedFatalf(t, "setting proxy resources failed", "unexpected %s memory request: expected %s, was %s", expected.pod, expected.memRequest, memRequestStr)
		}
	}
}

func TestVersionPostInstall(t *testing.T) {
	err := TestHelper.CheckVersion(TestHelper.GetVersion())
	if err != nil {
		testutil.AnnotatedFatalf(t, "Version command failed",
			"Version command failed\n%s", err.Error())
	}
}

func testCheckCommand(t *testing.T, stage string, expectedVersion string, namespace string, cliVersionOverride string, compareOutput bool) {
	var cmd []string
	var golden string
	if stage == "proxy" {
		cmd = []string{"check", "--proxy", "--expected-version", expectedVersion, "--namespace", namespace, "--wait=0"}
		// if TestHelper.GetMulticlusterHelmReleaseName() != "" || TestHelper.Multicluster() {
		// golden = "check.multicluster.proxy.golden"
		// } else if TestHelper.CNI() {
		if TestHelper.CNI() {
			golden = "check.cni.proxy.golden"
		} else {
			golden = "check.proxy.golden"
		}
	} else if stage == "config" {
		cmd = []string{"check", "config", "--expected-version", expectedVersion, "--wait=0"}
		golden = "check.config.golden"
	} else {
		cmd = []string{"check", "--expected-version", expectedVersion, "--wait=0"}
		// if TestHelper.GetMulticlusterHelmReleaseName() != "" || TestHelper.Multicluster() {
		// golden = "check.multicluster.golden"
		// } else if TestHelper.CNI() {
		if TestHelper.CNI() {
			golden = "check.cni.golden"
		} else {
			golden = "check.golden"
		}
	}

	timeout := time.Minute
	err := TestHelper.RetryFor(timeout, func() error {
		if cliVersionOverride != "" {
			cliVOverride := []string{"--cli-version-override", cliVersionOverride}
			cmd = append(cmd, cliVOverride...)
		}
		out, err := TestHelper.LinkerdRun(cmd...)

		if err != nil {
			return fmt.Errorf("'linkerd check' command failed\n%s", err)
		}

		if !compareOutput {
			return nil
		}

		err = TestHelper.ValidateOutput(out, golden)
		if err != nil {
			return fmt.Errorf("received unexpected output\n%s", err.Error())
		}

		return nil
	})
	if err != nil {
		testutil.AnnotatedFatal(t, fmt.Sprintf("'linkerd check' command timed-out (%s)", timeout), err)
	}
}

// TODO: run this after a `linkerd install config`
func TestCheckConfigPostInstall(t *testing.T) {
	testCheckCommand(t, "config", TestHelper.GetVersion(), "", "", true)
}

func TestCheckPostInstall(t *testing.T) {
	testCheckCommand(t, "", TestHelper.GetVersion(), "", "", true)
}

func TestUpgradeTestAppWorksAfterUpgrade(t *testing.T) {
	if TestHelper.UpgradeFromVersion() != "" {
		testAppNamespace := "upgrade-test"
		if err := testutil.ExerciseTestAppEndpoint("/api/vote?choice=:policeman:", testAppNamespace, TestHelper); err != nil {
			testutil.AnnotatedFatalf(t, "error exercising test app endpoint after upgrade",
				"error exercising test app endpoint after upgrade %s", err)
		}
	} else {
		t.Skip("Skipping for non upgrade test")
	}
}

func TestInstallSP(t *testing.T) {
	cmd := []string{"install-sp"}

	out, err := TestHelper.LinkerdRun(cmd...)
	if err != nil {
		testutil.AnnotatedFatal(t, "'linkerd install-sp' command failed", err)
	}

	out, err = TestHelper.KubectlApply(out, TestHelper.GetLinkerdNamespace())
	if err != nil {
		testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
			"'kubectl apply' command failed\n%s", out)
	}
}

func TestDashboard(t *testing.T) {
	dashboardPort := 52237
	dashboardURL := fmt.Sprintf("http://localhost:%d", dashboardPort)

	outputStream, err := TestHelper.LinkerdRunStream("viz", "dashboard", "-p",
		strconv.Itoa(dashboardPort), "--show", "url")
	if err != nil {
		testutil.AnnotatedFatalf(t, "error running command",
			"error running command:\n%s", err)
	}
	defer outputStream.Stop()

	outputLines, err := outputStream.ReadUntil(4, 1*time.Minute)
	if err != nil {
		testutil.AnnotatedFatalf(t, "error running command",
			"error running command:\n%s", err)
	}

	output := strings.Join(outputLines, "")
	if !strings.Contains(output, dashboardURL) {
		testutil.AnnotatedFatalf(t,
			"dashboard command failed. Expected url [%s] not present", dashboardURL)
	}

	resp, err := TestHelper.HTTPGetURL(dashboardURL + "/api/version")
	if err != nil {
		testutil.AnnotatedFatalf(t, "unexpected error",
			"unexpected error: %v", err)
	}

	if !strings.Contains(resp, TestHelper.GetVersion()) {
		testutil.AnnotatedFatalf(t, "dashboard command failed; response doesn't contain expected version",
			"dashboard command failed. Expected response [%s] to contain version [%s]",
			resp, TestHelper.GetVersion())
	}
}

func TestInject(t *testing.T) {
	resources, err := testutil.ReadFile("testdata/smoke_test.yaml")
	if err != nil {
		testutil.AnnotatedFatalf(t, "failed to read smoke test file",
			"failed to read smoke test file: %s", err)
	}

	ctx := context.Background()
	for _, tc := range injectionCases {
		tc := tc // pin
		t.Run(tc.ns, func(t *testing.T) {
			var out string

			prefixedNs := TestHelper.GetTestNamespace(tc.ns)

			err := TestHelper.CreateDataPlaneNamespaceIfNotExists(ctx, prefixedNs, tc.annotations)
			if err != nil {
				testutil.AnnotatedFatalf(t, fmt.Sprintf("failed to create %s namespace", prefixedNs),
					"failed to create %s namespace: %s", prefixedNs, err)
			}

			if tc.injectArgs != nil {
				cmd := []string{"inject"}
				cmd = append(cmd, tc.injectArgs...)
				cmd = append(cmd, "testdata/smoke_test.yaml")

				var injectReport string
				out, injectReport, err = TestHelper.PipeToLinkerdRun("", cmd...)
				if err != nil {
					testutil.AnnotatedFatalf(t, "'linkerd inject' command failed",
						"'linkerd inject' command failed: %s\n%s", err, out)
				}

				err = TestHelper.ValidateOutput(injectReport, "inject.report.golden")
				if err != nil {
					testutil.AnnotatedFatalf(t, "received unexpected output",
						"received unexpected output\n%s", err.Error())
				}
			} else {
				out = resources
			}

			out, err = TestHelper.KubectlApply(out, prefixedNs)
			if err != nil {
				testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
					"'kubectl apply' command failed\n%s", out)
			}

			for _, deploy := range []string{"smoke-test-terminus", "smoke-test-gateway"} {
				if err := TestHelper.CheckPods(ctx, prefixedNs, deploy, 1); err != nil {
					if rce, ok := err.(*testutil.RestartCountError); ok {
						testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
					} else {
						testutil.AnnotatedFatal(t, "CheckPods timed-out", err)
					}
				}
			}

			url, err := TestHelper.URLFor(ctx, prefixedNs, "smoke-test-gateway", 8080)
			if err != nil {
				testutil.AnnotatedFatalf(t, "failed to get URL",
					"failed to get URL: %s", err)
			}

			output, err := TestHelper.HTTPGetURL(url)
			if err != nil {
				testutil.AnnotatedFatalf(t, "unexpected error",
					"unexpected error: %v %s", err, output)
			}

			expectedStringInPayload := "\"payload\":\"BANANA\""
			if !strings.Contains(output, expectedStringInPayload) {
				testutil.AnnotatedFatalf(t, "application response doesn't contain the expected response",
					"expected application response to contain string [%s], but it was [%s]",
					expectedStringInPayload, output)
			}
		})
	}
}

func TestServiceProfileDeploy(t *testing.T) {
	bbProto, err := TestHelper.HTTPGetURL("https://raw.githubusercontent.com/BuoyantIO/bb/v0.0.5/api.proto")
	if err != nil {
		testutil.AnnotatedFatalf(t, "unexpected error",
			"unexpected error: %v %s", err, bbProto)
	}

	for _, tc := range injectionCases {
		tc := tc // pin
		t.Run(tc.ns, func(t *testing.T) {
			prefixedNs := TestHelper.GetTestNamespace(tc.ns)

			cmd := []string{"profile", "-n", prefixedNs, "--proto", "-", "smoke-test-terminus-svc"}
			bbSP, stderr, err := TestHelper.PipeToLinkerdRun(bbProto, cmd...)
			if err != nil {
				testutil.AnnotatedFatalf(t, "unexpected error",
					"unexpected error: %v %s", err, stderr)
			}

			out, err := TestHelper.KubectlApply(bbSP, prefixedNs)
			if err != nil {
				testutil.AnnotatedFatalf(t, "'kubectl apply' command failed",
					"'kubectl apply' command failed: %s\n%s", err, out)
			}
		})
	}
}

func TestCheckProxy(t *testing.T) {
	for _, tc := range injectionCases {
		tc := tc // pin
		t.Run(tc.ns, func(t *testing.T) {
			prefixedNs := TestHelper.GetTestNamespace(tc.ns)
			testCheckCommand(t, "proxy", TestHelper.GetVersion(), prefixedNs, "", true)
		})
	}
}

func TestRestarts(t *testing.T) {
	for deploy, spec := range testutil.LinkerdDeployReplicasEdge {
		if err := TestHelper.CheckPods(context.Background(), spec.Namespace, deploy, spec.Replicas); err != nil {
			if rce, ok := err.(*testutil.RestartCountError); ok {
				testutil.AnnotatedWarn(t, "CheckPods timed-out", rce)
			} else {
				testutil.AnnotatedFatal(t, "CheckPods timed-out", err)
			}
		}
	}
}

func TestCheckMulticluster(t *testing.T) {
	if TestHelper.GetMulticlusterHelmReleaseName() != "" || TestHelper.Multicluster() {
		cmd := []string{"multicluster", "check", "--wait=0"}
		golden := "check.multicluster.golden"
		timeout := time.Minute
		err := TestHelper.RetryFor(timeout, func() error {
			out, err := TestHelper.LinkerdRun(cmd...)
			if err != nil {
				return fmt.Errorf("'linkerd multicluster check' command failed\n%s", err)
			}
			err = TestHelper.ValidateOutput(out, golden)
			if err != nil {
				return fmt.Errorf("received unexpected output\n%s", err.Error())
			}
			return nil
		})
		if err != nil {
			testutil.AnnotatedFatal(t, fmt.Sprintf("'linkerd multicluster check' command timed-out (%s)", timeout), err)
		}
	} else {
		t.Skip("Skipping for non multicluster test")
	}
}
