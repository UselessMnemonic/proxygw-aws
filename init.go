package aws

import (
	"github.com/UselessMnemonic/proxygw/pkg/engine"
	"github.com/UselessMnemonic/proxygw/plugin"
	"github.com/UselessMnemonic/proxygw-aws/targets"
)

func init() {
	err := plugin.Register("aws", plugin.Handler{
		OnLoad: func(_ map[string]any, _ *engine.Engine, namespace *plugin.Namespace) error {
			namespace.Targets["ec2"] = targets.NewEC2Handler
			return nil
		},
		OnUnload: func() error {
			return nil
		},
	})
	if err != nil {
		panic(err)
	}
}
