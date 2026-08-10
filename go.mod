module github.com/domdom82/udpmux

go 1.26.5

require (
	github.com/gardener/gardener v1.147.1
	github.com/go-logr/logr v1.4.4
	github.com/spf13/cobra v1.10.2
	go.uber.org/zap v1.28.0
	golang.org/x/net v0.57.0
	golang.org/x/sync v0.22.0
	k8s.io/component-base v0.36.2
	k8s.io/klog/v2 v2.140.0
	sigs.k8s.io/controller-runtime v0.24.1
)

replace github.com/gardener/gardener/pkg/apis v0.0.0 => github.com/gardener/gardener/pkg/apis v1.147.1

require (
	github.com/fxamacker/cbor/v2 v2.9.2 // indirect
	github.com/go-logr/zapr v1.3.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/json-iterator/go v1.1.13-0.20220915233716-71ac16282d12 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.yaml.in/yaml/v2 v2.4.4 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	k8s.io/apimachinery v0.36.2 // indirect
	k8s.io/kube-openapi v0.0.0-20260603220949-865597e52e25 // indirect
	k8s.io/utils v0.0.0-20260707023825-cf1189d6abe3 // indirect
	sigs.k8s.io/json v0.0.0-20250730193827-2d320260d730 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v6 v6.4.0 // indirect
)
