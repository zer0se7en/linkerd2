package cmd

import (
	"fmt"
	"regexp"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	defaultLinkerdNamespace = "linkerd"
	defaultJaegerNamespace  = "linkerd-jaeger"
)

var (
	apiAddr               string // An empty value means "use the Kubernetes configuration"
	controlPlaneNamespace string
	namespace             string
	kubeconfigPath        string
	kubeContext           string
	impersonate           string
	impersonateGroup      []string
	verbose               bool

	// These regexs are not as strict as they could be, but are a quick and dirty
	// sanity check against illegal characters.
	alphaNumDash = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)
)

// NewCmdJaeger returns a new jeager command
func NewCmdJaeger() *cobra.Command {
	jaegerCmd := &cobra.Command{
		Use:   "jaeger",
		Short: "jaeger manages the jaeger extension of Linkerd service mesh",
		Long:  `jaeger manages the jaeger extension of Linkerd service mesh.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// enable / disable logging
			if verbose {
				log.SetLevel(log.DebugLevel)
			} else {
				log.SetLevel(log.PanicLevel)
			}

			if !alphaNumDash.MatchString(controlPlaneNamespace) {
				return fmt.Errorf("%s is not a valid namespace", controlPlaneNamespace)
			}

			return nil
		},
	}

	jaegerCmd.PersistentFlags().StringVarP(&controlPlaneNamespace, "linkerd-namespace", "L", defaultLinkerdNamespace, "Namespace in which Linkerd is installed")
	jaegerCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", defaultJaegerNamespace, "Namespace in which Jaeger extension is installed")
	jaegerCmd.PersistentFlags().StringVar(&kubeconfigPath, "kubeconfig", "", "Path to the kubeconfig file to use for CLI requests")
	jaegerCmd.PersistentFlags().StringVar(&kubeContext, "context", "", "Name of the kubeconfig context to use")
	jaegerCmd.PersistentFlags().StringVar(&impersonate, "as", "", "Username to impersonate for Kubernetes operations")
	jaegerCmd.PersistentFlags().StringArrayVar(&impersonateGroup, "as-group", []string{}, "Group to impersonate for Kubernetes operations")
	jaegerCmd.PersistentFlags().StringVar(&apiAddr, "api-addr", "", "Override kubeconfig and communicate directly with the control plane at host:port (mostly for testing)")
	jaegerCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "Turn on debug logging")
	jaegerCmd.AddCommand(newCmdInstall())
	jaegerCmd.AddCommand(newCmdUninstall())
	jaegerCmd.AddCommand(newCmdDashboard())

	return jaegerCmd
}
