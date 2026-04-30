package aws

import (
	"github.com/UselessMnemonic/proxygw-aws/targets"
	"github.com/UselessMnemonic/proxygw/pkg/engine"
	"github.com/UselessMnemonic/proxygw/plugin"
)

func init() {
	plugin.Register("github.com/UselessMnemonic/proxygw-aws", plugin.Handler{
		OnLoad: func(_ map[string]any, _ *engine.Engine, namespace *plugin.Namespace) error {
			namespace.Targets["ec2"] = targets.NewEC2Handler
			return nil
		},
		OnUnload: func() error {
			return nil
		},
	})
}
