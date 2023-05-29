# Build your own Kustomize transformers using KRM functions

Kustomize is a great tool that allows you to patch, compose & transform kubernetes manifests without templating.

If you haven't heard about it before, checkout their website and give it a spin: https://kustomize.io/

I find this tool particularly nice because it makes the whole setup composable and open, empowering the developers to customize (wink) their deployments without having to deal with upstream templates.

One downside though is that while kustomize offers a large set of transformations (set images, replicas, apply a JSON patch to any manifest...), it might not be sufficient for all your needs. In particular if you want your platform team to offer some extra toolings to reduce boilerplate and improve developer experience.

That's where the new kustomize plugin functionality comes in!

In this blog post, I'll walk you through how to use & write your own KRM functions to be used as kustomize transformers, but first...

## What are KRM functions?

KRM function have been introduced by the KPT tool (https://kpt.dev/book/02-concepts/03-functions) and are starting to get traction in the broader community. The idea is to provide the capability to have generic client-side transformations of kubernetes manifests packaged as containers.

You can find the specification for such function here: https://github.com/kubernetes-sigs/kustomize/blob/master/cmd/config/docs/api-conventions/functions-spec.md

The way they work is they take 2 sets of inputs: their own manifests, and a list of kubernetes manifests. They then return a new list of kubernetes manifests as output, which will be used for the following sets of transformations.

This allows for a conceptual simple and very composable pipeline of transformations.

The nice thing is that the latest version of kustomize allows us to use KRM functions that can be used during `kustomize build`!

## How to use a KRM function as a transformer?

Let's start from the consumer perspective first. In order to use a KRM function as a transformer, you first need to write a manifest that contains:
1. the container image of a KRM function
2. the inputs to that function.

Imagine we have a KRM function that injects a sidecar to all kubernetes workloads containing a `kustomize.treyd.io/cloud-sql-proxy` annotation.

It takes as input the image & version of the proxy, plus a list of database instances (for the curious, this is pretty much the code we're using in production at Treyd to inject a [cloud-sql-proxy](https://github.com/GoogleCloudPlatform/cloud-sql-proxy) sidecar to our pods).

The manifest will look something like this:

```yaml
# transformer.yaml

# api version, kind and name are mostly placeholders, they are only required for this to be a valid manifest
apiVersion: examples.config.kubernetes.io/v1beta1
kind: inject-cloud-sql-proxy
metadata:
  name: inject-cloud-sql-proxy
  annotations:
    # Specify the location of the KRM function, in this case a docker image
    config.kubernetes.io/function: |-
      container:
        image: ghcr.io/treyd-io/blog-kustomize-krm-function-demo:main
spec:
  # Specify the inputs to the function
  proxyImage: gcr.io/cloud-sql-connectors/cloud-sql-proxy
  proxyVersion: 2.0.0
  proxyInstances: your-project:your-region:your-instance=tcp:5432
```

Then you can add it as a transformer in your `kustomization.yaml` file:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  - resources.yaml

transformers:
  - transformer.yaml
```

Then you can run `kustomize build .` in the same folder to see the result of the transformation!

One caveat: currently, this feature is behind a feature flag because it's still early, so you need to run `kustomize build --enable-alpha-plugins .` instead.

Another caveat is that this feature is not yet available in the kustomize version packaged with `kubectl`, so you won't be able to us this with `kubectl apply -k .` just yet. You'll need to use `kustomize build --enable-alpha-plugins . | kubectl apply -f-` until support is added.


If you don't want to write a separaet file just for the transformation, you can also just declare the transformer inline in your `kustomization.yaml`:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  - resources.yaml

transformers:
  - |-
    apiVersion: examples.config.kubernetes.io/v1beta1
    kind: inject-cloud-sql-proxy
    metadata:
      name: inject-cloud-sql-proxy
      annotations:
        config.kubernetes.io/function: |-
          container:
            image: ghcr.io/treyd-io/blog-kustomize-krm-function-demo:main
    spec:
      proxyImage: europe-north1-docker.pkg.dev/treyd-docker-images/main/gce-proxy
      proxyVersion: sha-f2bc7002b5b0da04ef60e797922fd8b0f1a5fc25
      proxyInstances: treyd-staging:europe-north1:treyd-staging-db=tcp:5432
```

The annoying part with this is still quite verbose if you have to write that in all your projects...

But the killer feature is you can also use a remote transformer hosted in a repository on github!

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  - resources.yaml

transformers:
  - https://github.com/treyd-io/blog-kustomize-krm-function-demo.git/config/staging
```

That's the part where your developers can just consumer transformers provided in another repository by your platform team!

You can find those working examples here: https://github.com/treyd-io/blog-kustomize-krm-function-demo/tree/main/examples

## How to write a KRM function?

You can write it in any language really, as long as it handles the input & output according the the KRM spec. Kustomize provides a set of go libraries through the `kyaml` package, that also includes a bunch of helper functions that are useful for manipulation kubernetes manififests, so I'll be using that.

You can find the documentation for it here: https://pkg.go.dev/sigs.k8s.io/kustomize/kyaml/fn/framework

Plus golang is a bit the defactor language for platform tooling these days, so might as well stick to that!

So let's start by creating a new go project, and importing some libraries, and setup the boilerplate for a no-op transformer:

```sh
go mod init tooling.devops.io/krm-fn-inject-cloud-sql-proxy
go get sigs.k8s.io/kustomize/kyaml
```

Create a `main.go` file:
```go
// main.go
package main

import (
	"os"

	"sigs.k8s.io/kustomize/kyaml/fn/framework"
	"sigs.k8s.io/kustomize/kyaml/fn/framework/command"
	"sigs.k8s.io/kustomize/kyaml/kio"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

type API struct {
    // Declare function inputs here
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
}

func main() {
	api := new(API)

	fn := func(items []*yaml.RNode) ([]*yaml.RNode, error) {
        // Add, remove, transform items here
		return items, nil
	}

	p := framework.SimpleProcessor{Config: api, Filter: kio.FilterFunc(fn)}
	cmd := command.Build(p, command.StandaloneDisabled, false)
	command.AddGenerateDockerfile(cmd)
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
```

This is the boilerplate required to use the kyaml framework.

Next, let's build it:

```sh
go build
```

And generate a Dockerfile for it:

```
./krm-fn-inject-cloud-sql-proxy gen .
```

Your folder should look like this:

```
./Dockerfile
./go.mod
./go.sum
./krm-fn-inject-cloud-sql-proxy
./main.go
```

Now we can build a docker image for it:

```sh
docker build -t krm-fn-inject-cloud-sql-proxy .
```

Add it inside a kustomization file:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  - resources.yaml

transformers:
  - |-
    apiVersion: examples.config.kubernetes.io/v1beta1
    kind: demo
    metadata:
      name: test
      annotations:
        config.kubernetes.io/function: |-
          container:
            image: krm-fn-inject-cloud-sql-proxy
```

And run it:

```sh
kustomize build --enable-alpha-plugins .
```

This should return the manifests from `resources.yaml` unchanged.

Now let's add the logic for adding the sidecar. First, declare the function inputs in the API struct:

```diff
 type API struct {
 	Metadata struct {
 		Name string `yaml:"name"`
 	} `yaml:"metadata"`

+	Spec struct {
+		ProxyImage   *string `yaml:"proxyImage"`
+		ProxyVersion *string `yaml:"proxyVersion"`
+		ProxyInstances *string `yaml:"proxyInstances"`
+	} `yaml:"spec"`
 }
```

Then add a loop in the processor function to add the sidecar:

```diff
 func main() {
 	api := new(API)

 	fn := func(items []*yaml.RNode) ([]*yaml.RNode, error) {
        // Add, remove, transform items here
+		for _, item := range items {
+			err := addSidecar(*api, item)
+			if err != nil {
+				return nil, err
+			}
+		}
 		return items, nil
 	}
```

And then draw the rest of the Owl (insert meme image):

```go
func addSidecar(api API, r *yaml.RNode) error {
	meta, err := r.GetMeta()
	if err != nil {
		return err
	}

    // Check if the item's metadata contains the expected annotation flag
	if _, found := meta.Annotations[annotationFlag]; !found {
		return nil
	}

    // use kyaml to create a YAML object with the container to be injected
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

    // Find the location of the containers declaration in the manifest
    // (note that this works for all k8s workload, one of the advantages of using kyaml
	containers, err := r.Pipe(yaml.LookupFirstMatch(yaml.ConventionalContainerPaths))
	if err != nil {
		return err
	}

    // Prepend the sidecar container in the containers list
	newContainers := yaml.NewListRNode()
	newContainers.Pipe(yaml.Append(container.YNode()))
	for _, c := range containers.Content() {
		newContainers.Pipe(yaml.Append(c))
	}

	containers.SetYNode(newContainers.YNode())

	return nil
}
```

You can find a complete working example here: https://github.com/treyd-io/blog-kustomize-krm-function-demo/blob/main/main.go

## Conclusion

Kustomize KRM function plugins allows for a lot of flexibility using relatively simple tools. It also provides a potentially clear interface between the developers and the platform team, with the platform team providing a set of transformers that the developer can consume.

We went through examples on how to write & consume those tools, I hope I have inspired some of you to try it out!
