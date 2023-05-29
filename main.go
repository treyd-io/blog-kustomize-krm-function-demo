package main

import (
	"os"

	"sigs.k8s.io/kustomize/kyaml/fn/framework"
	"sigs.k8s.io/kustomize/kyaml/fn/framework/command"
	"sigs.k8s.io/kustomize/kyaml/kio"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

var annotationFlag = "kustomize.treyd.io/cloud-sql-proxy"

type API struct {
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`

	Spec struct {
		ProxyImage   *string `yaml:"proxyImage"`
		ProxyVersion *string `yaml:"proxyVersion"`
		ProxyInstances *string `yaml:"proxyInstances"`
	} `yaml:"spec"`
}

func main() {
	api := new(API)

	fn := func(items []*yaml.RNode) ([]*yaml.RNode, error) {
		for _, item := range items {
			err := addSidecar(*api, item)
			if err != nil {
				return nil, err
			}
		}
		return items, nil
	}

	p := framework.SimpleProcessor{Config: api, Filter: kio.FilterFunc(fn)}
	cmd := command.Build(p, command.StandaloneDisabled, false)
	command.AddGenerateDockerfile(cmd)
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func addSidecar(api API, r *yaml.RNode) error {
	meta, err := r.GetMeta()
	if err != nil {
		return err
	}

	if _, found := meta.Annotations[annotationFlag]; !found {
		return nil
	}

	command := yaml.NewListRNode(proxyCommand(api, r.GetKind())...)

	container := yaml.NewMapRNode(nil)
	container.Pipe(
		yaml.Tee(yaml.SetField("name", yaml.NewStringRNode("proxysql"))),
		yaml.Tee(yaml.SetField("image", yaml.NewStringRNode(proxyImage(api)))),
		yaml.Tee(yaml.SetField("command", command)),
		yaml.Tee(
			yaml.LookupCreate(yaml.MappingNode, "securityContext"),
			yaml.SetField("runAsNonRoot", yaml.MustParse("true")),
		),
		yaml.Tee(
			yaml.LookupCreate(yaml.MappingNode, "lifecycle"),
			yaml.LookupCreate(yaml.MappingNode, "postStart"),
			yaml.LookupCreate(yaml.MappingNode, "exec"),
			yaml.SetField("command", yaml.NewListRNode(
				"/bin/bash",
				"-c",
				"wait-for-port 5432",
			)),
		),
	)

	containers, err := r.Pipe(yaml.LookupFirstMatch(yaml.ConventionalContainerPaths))
	if err != nil {
		return err
	}

	newContainers := yaml.NewListRNode()
	newContainers.Pipe(yaml.Append(container.YNode()))
	for _, c := range containers.Content() {
		newContainers.Pipe(yaml.Append(c))
	}

	containers.SetYNode(newContainers.YNode())

	return nil
}

func proxyImage(api API) string {
	return *api.Spec.ProxyImage + ":" + *api.Spec.ProxyVersion
}

func proxyCommand(api API, kind string) []string {
	sqlProxyCommand := []string{
		"/cloud_sql_proxy",
		"-term_timeout=3600s",
		"-ip_address_types=PRIVATE",
		"-log_debug_stdout",
		"-instances=" + *api.Spec.ProxyInstances,
		"--enable_iam_login",
	}

	return sqlProxyCommand
}
