# kube-ecr-refresher

A helper tool for refreshing Amazon ECR credentials in Kubernetes clusters periodically.

# Motivation

Authorization tokens for [Amazon ECR](https://aws.amazon.com/ecr/) expire [every 12h](https://docs.aws.amazon.com/AmazonECR/latest/userguide/Registries.html).
This makes it cumbersome to pull images from Amazon ECR outside [Amazon EKS](https://aws.amazon.com/eks/), as [image pull secrets](https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry/) must be refreshed (in each required namespace) with the same frequency to ensure uninterrupted access.
`kube-ecr-refresher` takes care of automating this task by periodically creating/updating secrets containing fresh credentials in all namespaces (or just a subset of them) based on a given set of AWS credentials.

# Prerequisites

* A set of AWS credentials (`AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY`) having the `AmazonEC2ContainerRegistryReadOnly` policy attached to them.

# Installing

To install `kube-ecr-refresher`, start by running

```shell
$ kubectl apply -f deploy/common.yaml
namespace/kube-ecr-refresher created
serviceaccount/kube-ecr-refresher created
clusterrole.rbac.authorization.k8s.io/kube-ecr-refresher created
clusterrolebinding.rbac.authorization.k8s.io/kube-ecr-refresher created
```

Then, edit `deploy/secret.yaml` in order to specify the aforementioned AWS credentials and run

```shell
$ kubectl apply -f deploy/secret.yaml
secret/kube-ecr-refresher created
```

Finally, run

```shell
$ kubectl apply -f deploy/deployment.yaml
deployment.apps/kube-ecr-refresher created
```

and make sure that `kube-ecr-refresher` is indeed running:

```shell
$ kubectl -n kube-ecr-refresher get pod -l app=kube-ecr-refresher
NAME                                  READY   STATUS    RESTARTS   AGE
kube-ecr-refresher-7dbcf68bc9-cn99c   1/1     Running   0          2s
```

# Advanced

## Customizing the target namespaces

By default, `kube-ecr-refresher` created/updates image pull secrets across all namespaces in the Kubernetes cluster.
To create/update these secrets in just a subset of namespaces, add the `--target-namespaces` flag to the deployment and specify the desired namespaces as a comma-separated list.

## Refreshing credentials for multiple Amazon ECR registries

By design, `kube-ecr-refresher` supports refreshing credentials for a single Amazon ECR registry (i.e. the one associated with the provided AWS credentials).
If you need to pull images from multiple Amazon ECR registries in the same Kubernetes cluster, you must deploy an instance of `kube-ecr-refresher` per target Amazon ECR registry/set of AWS credentials. 

# License

Copyright 2019 Form3 Financial Cloud

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

   http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
