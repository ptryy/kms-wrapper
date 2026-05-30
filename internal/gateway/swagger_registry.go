package gateway

import (
	"github.com/ryan-truong/kms-wrapper/docs"
	swagv1 "github.com/swaggo/swag"
)

func init() {
	swagv1.Register(docs.SwaggerInfo.InstanceName(), docs.SwaggerInfo)
}
