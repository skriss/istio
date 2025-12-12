# Deploying Istio with OVN-K Primary UDNs

This README walks through creating two ClusterUserDefinedNetworks on an OCP cluster and installing one instance of Istio per CUDN.

## Prerequisites

An existing OCP cluster with OVN-K installed with User-Defined Network / Cluster User-Defined Network support. This walkthrough was tested on a 4.19 cluster.

## Steps

1. Clone the Sail Operator repo and install the operator.

    ```bash
    git clone https://github.com/istio-ecosystem/sail-operator.git
    cd sail-operator
    git checkout release-1.28
    make deploy
    ```

1. Create the necessary namespaces.

    ```bash
    kubectl create namespace istio-cni

    cat <<EOF | oc apply -f -
    apiVersion: v1
    kind: Namespace
    metadata:
      name: istio-system-red
      labels:
        k8s.ovn.org/primary-user-defined-network: ""
        istio-discovery: red
    EOF

    cat <<EOF | oc apply -f -
    apiVersion: v1
    kind: Namespace
    metadata:
      name: istio-system-blue
      labels:
        k8s.ovn.org/primary-user-defined-network: ""
        istio-discovery: blue
    EOF

    namespaces_with_injection=("sleep-red" "httpbin-red")
    for ns in "${namespaces_with_injection[@]}"; do
    cat <<EOF | oc apply -f -
    apiVersion: v1
    kind: Namespace
    metadata:
      name: $ns
      labels:
        k8s.ovn.org/primary-user-defined-network: ""
        istio.io/rev: istio-red
        istio-discovery: red
    EOF
    done

    namespaces_with_injection=("sleep-blue" "httpbin-blue")
    for ns in "${namespaces_with_injection[@]}"; do
    cat <<EOF | oc apply -f -
    apiVersion: v1
    kind: Namespace
    metadata:
      name: $ns
      labels:
        k8s.ovn.org/primary-user-defined-network: ""
        istio.io/rev: istio-blue
        istio-discovery: blue
    EOF
    done
    ```

1. Create the ClusterUserDefinedNetworks (CUDNs).

    ```bash
    cat <<EOF | oc apply -f -
    apiVersion: k8s.ovn.org/v1
    kind: ClusterUserDefinedNetwork
    metadata:
      name: cudn-red
    spec:
      namespaceSelector:
        matchLabels:
          istio-discovery: red
      network:
        topology: Layer3
        layer3:
          role: Primary
          subnets:
            - cidr: 22.222.0.0/16
              hostSubnet: 24
    EOF

    cat <<EOF | oc apply -f -
    apiVersion: k8s.ovn.org/v1
    kind: ClusterUserDefinedNetwork
    metadata:
      name: cudn-blue
    spec:     
      namespaceSelector:
        matchLabels:
          istio-discovery: blue
      network:
        topology: Layer3
        layer3:
          role: Primary
          subnets:
            - cidr: 22.233.0.0/16
              hostSubnet: 24
    EOF
    ```

1. Deploy istio-cni.

    ```bash
    cat <<EOF | oc apply -f -
    apiVersion: sailoperator.io/v1
    kind: IstioCNI
    metadata:
      name: default
    spec:
      version: v1.28.1
      namespace: istio-cni
    EOF
    ```

1. Deploy two instances of istiod.

    ```bash
    cat <<EOF | oc apply -f -
    apiVersion: sailoperator.io/v1
    kind: Istio
    metadata:
      name: istio-red
    spec:
      version: v1.28.1
      namespace: istio-system-red
      updateStrategy:
        type: InPlace
        inactiveRevisionDeletionGracePeriodSeconds: 30
      values:
        pilot:
          image: quay.io/skriss/pilot:ovnk-udn
          podAnnotations:
            k8s.ovn.org/open-default-ports: |
              - protocol: tcp
                port: 15017
              - protocol: tcp
                port: 15012
              - protocol: tcp
                port: 8080
              - protocol: tcp
                port: 15010
              - protocol: tcp
                port: 15014
        global:
          imagePullPolicy: "Always"
        meshConfig:
          accessLogFile: /dev/stdout
          discoverySelectors:
            - matchLabels:
                istio-discovery: red       
    EOF

    cat <<EOF | oc apply -f -
    apiVersion: sailoperator.io/v1
    kind: Istio
    metadata:
      name: istio-blue
    spec:
      version: v1.28.1
      namespace: istio-system-blue
      updateStrategy:
        type: InPlace
        inactiveRevisionDeletionGracePeriodSeconds: 30
      values:
        pilot:
          image: quay.io/skriss/pilot:ovnk-udn
          podAnnotations:
            k8s.ovn.org/open-default-ports: |
              - protocol: tcp
                port: 15017
              - protocol: tcp
                port: 15012
              - protocol: tcp
                port: 8080
              - protocol: tcp
                port: 15010
              - protocol: tcp
                port: 15014
        global:
          imagePullPolicy: "Always"
        meshConfig:
          accessLogFile: /dev/stdout
          discoverySelectors:
            - matchLabels:
                istio-discovery: blue
    EOF
    ```

1. Deploy the `sleep` and `httpbin` workloads to both CUDNs.

    ```bash
    oc apply -n sleep-red -f https://raw.githubusercontent.com/istio/istio/release-1.20/samples/sleep/sleep.yaml
    oc apply -n httpbin-red -f https://raw.githubusercontent.com/sridhargaddam/istio-workspace/refs/heads/main/sample-yamls-ambient/httpbin.yaml

    oc apply -n sleep-blue -f https://raw.githubusercontent.com/istio/istio/release-1.20/samples/sleep/sleep.yaml
    oc apply -n httpbin-blue -f https://raw.githubusercontent.com/sridhargaddam/istio-workspace/refs/heads/main/sample-yamls-ambient/httpbin.yaml
    ```

1. Test connectivity between workloads in the same CUDN.

    ```bash
    kubectl exec -it -n sleep-red deploy/sleep -- curl -s httpbin.httpbin-red.svc.cluster.local:8000/get

    kubectl exec -it -n sleep-blue deploy/sleep -- curl -s httpbin.httpbin-blue.svc.cluster.local:8000/get
    ```

1. Test that workloads cannot connect to workloads in other CUDNS.

    ```bash
    kubectl exec -it -n sleep-blue deploy/sleep -- curl -s httpbin.httpbin-red.svc.cluster.local:8000/get

    kubectl exec -it -n sleep-red deploy/sleep -- curl -s httpbin.httpbin-blue.svc.cluster.local:8000/get
    ```

1. Check the istio-proxy logs to view access logs.

    ```bash
    kubectl logs -n httpbin-red deploy/httpbin -c istio-proxy | grep GET
    kubectl logs -n httpbin-blue deploy/httpbin -c istio-proxy | grep GET
    ```

1. Inspect Istio endpoints to verify that they correspond to CUDN IPs (i.e. in the `22.` range), and that only workloads from the same CUDN appear.

    ```bash
    istioctl proxy-config endpoints deploy/sleep -n sleep-red | grep httpbin

    istioctl proxy-config endpoints deploy/sleep -n sleep-blue | grep httpbin
    ```