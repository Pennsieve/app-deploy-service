package provisioner

import "context"

type Provisioner interface {
	Run(context.Context) error
}
