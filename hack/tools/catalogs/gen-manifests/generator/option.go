package generator

import (
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
)

func AddFlags(cmd *cobra.Command) *Options {
	opts := &Options{
		WatchNamespace: corev1.NamespaceAll,
	}
	cmd.Flags().StringVarP(&opts.WatchNamespace, "watch-namespace", "w", "", "Cluster extension watch namespace. If empty, watches all namespaces.")
	return opts
}

type Options struct {
	WatchNamespace         string
	installNamespace       string
	clusterExtensionName   string
	serviceAccountName     string
	clusterRoleName        string
	clusterRoleBindingName string
	roleName               string
	roleBindingName        string
}
