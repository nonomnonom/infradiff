// Package kubernetes parses Kubernetes YAML and JSON manifests into graphs.
package kubernetes

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nonomnonom/infradiff/internal/model"
	"github.com/nonomnonom/infradiff/internal/redact"
	"gopkg.in/yaml.v3"
)

// Parser implements parser.Parser for Kubernetes manifests.
type Parser struct{}

// New returns a Kubernetes parser.
func New() *Parser { return &Parser{} }

// Name returns the parser name used in diagnostics.
func (p *Parser) Name() string { return "kubernetes" }

// Detect reports whether a YAML or JSON file contains Kubernetes objects.
func (p *Parser) Detect(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml", ".json":
	default:
		return false
	}
	documents, err := readDocuments(path)
	if err != nil {
		return false
	}
	for _, document := range documents {
		header, err := decodeHeader(document)
		if err != nil {
			continue
		}
		if header.APIVersion != "" && header.Kind != "" {
			return true
		}
	}
	return false
}

// Parse reads every supported document in a manifest file.
func (p *Parser) Parse(path string) (*model.Graph, error) {
	return parseSources([]string{path})
}

// ParseDir reads all supported manifests directly inside dir. References can
// span files in the same directory, which is how most applications ship.
func (p *Parser) ParseDir(dir string) (*model.Graph, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("kubernetes: %s: %w", dir, err)
	}
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if p.Detect(path) {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("kubernetes: %s: no supported manifests found", dir)
	}
	return parseSources(paths)
}

func parseSources(paths []string) (*model.Graph, error) {
	sources, err := readDocumentSources(paths)
	if err != nil {
		return nil, fmt.Errorf("kubernetes: %w", err)
	}

	graph := model.New()
	groups := make(map[string]bool)
	seen := make(map[string]string)

	var workloads []workloadInfo
	var services []serviceInfo
	var references []referenceInfo
	var ingressTargets []ingressInfo
	var scaleTargets []scaleTargetInfo
	objects := make(map[string]bool)
	supported := 0

	for _, document := range sources {
		path := document.path
		root := documentRoot(document.node)
		if root == nil {
			continue
		}
		header, err := decodeHeader(root)
		if err != nil {
			return nil, fmt.Errorf("kubernetes: %s:%d: %w", path, root.Line, err)
		}
		if header.APIVersion == "" || header.Kind == "" {
			continue
		}

		name := header.Metadata.Name
		if name == "" {
			return nil, fmt.Errorf("kubernetes: %s:%d: %s has no metadata.name; add one so the node can be identified",
				path, root.Line, header.Kind)
		}

		namespace := header.Metadata.Namespace
		clusterScoped := isClusterScoped(header.Kind)
		if !clusterScoped && namespace == "" && knownNamespaced[header.Kind] {
			namespace = "default"
		}
		if clusterScoped {
			namespace = ""
		}
		groupID := ""
		if namespace != "" {
			groupID = "group:namespace:" + namespace
			if !groups[groupID] {
				groups[groupID] = true
				graph.Groups = append(graph.Groups, model.Group{
					ID:     groupID,
					Name:   namespace,
					Kind:   model.GroupNamespace,
					Status: model.StatusUnchanged,
				})
			}
		}

		id := objectIDFor(header.Kind, namespace, name)
		if previous, exists := seen[id]; exists {
			return nil, fmt.Errorf("kubernetes: %s:%d: %s %q is already defined at %s",
				path, root.Line, header.Kind, name, previous)
		}
		seen[id] = fmt.Sprintf("%s:%d", path, root.Line)
		supported++

		var nodeType model.NodeType
		metadata := map[string]string{}
		line := root.Line

		switch header.Kind {
		case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet":
			var workloadDoc workloadDocument
			if err := root.Decode(&workloadDoc); err != nil {
				return nil, fmt.Errorf("kubernetes: %s:%d: %s %q: %w", path, line, header.Kind, name, err)
			}
			nodeType = model.NodeCompute
			addPodTemplateMetadata(metadata, workloadDoc.Spec.Template)
			if workloadDoc.Spec.Replicas != nil {
				metadata["replicas"] = fmt.Sprintf("%d", *workloadDoc.Spec.Replicas)
			}
			labels := mergeLabels(workloadDoc.Spec.Template.Metadata.Labels, workloadDoc.Spec.Selector.MatchLabels)
			workloads = append(workloads, workloadInfo{id: id, namespace: namespace, labels: labels})
			for _, ref := range podReferences(workloadDoc.Spec.Template.Spec) {
				references = append(references, referenceInfo{
					from: id, namespace: namespace, kind: ref.kind, name: ref.name,
				})
			}

		case "Job":
			var job jobDocument
			if err := root.Decode(&job); err != nil {
				return nil, fmt.Errorf("kubernetes: %s:%d: Job %q: %w", path, line, name, err)
			}
			nodeType = model.NodeCompute
			addPodTemplateMetadata(metadata, job.Spec.Template)
			if job.Spec.Parallelism != nil {
				metadata["parallelism"] = fmt.Sprintf("%d", *job.Spec.Parallelism)
			}
			if job.Spec.Completions != nil {
				metadata["completions"] = fmt.Sprintf("%d", *job.Spec.Completions)
			}
			labels := job.Spec.Template.Metadata.Labels
			workloads = append(workloads, workloadInfo{id: id, namespace: namespace, labels: labels})
			for _, ref := range podReferences(job.Spec.Template.Spec) {
				references = append(references, referenceInfo{
					from: id, namespace: namespace, kind: ref.kind, name: ref.name,
				})
			}

		case "CronJob":
			var job cronJobDocument
			if err := root.Decode(&job); err != nil {
				return nil, fmt.Errorf("kubernetes: %s:%d: CronJob %q: %w", path, line, name, err)
			}
			nodeType = model.NodeCompute
			addPodTemplateMetadata(metadata, job.Spec.JobTemplate.Spec.Template)
			if job.Spec.Schedule != "" {
				metadata["schedule"] = job.Spec.Schedule
			}
			labels := job.Spec.JobTemplate.Spec.Template.Metadata.Labels
			workloads = append(workloads, workloadInfo{id: id, namespace: namespace, labels: labels})
			for _, ref := range podReferences(job.Spec.JobTemplate.Spec.Template.Spec) {
				references = append(references, referenceInfo{
					from: id, namespace: namespace, kind: ref.kind, name: ref.name,
				})
			}

		case "Pod":
			var pod podDocument
			if err := root.Decode(&pod); err != nil {
				return nil, fmt.Errorf("kubernetes: %s:%d: Pod %q: %w", path, line, name, err)
			}
			nodeType = model.NodeCompute
			addPodTemplateMetadata(metadata, podTemplate(pod))
			workloads = append(workloads, workloadInfo{id: id, namespace: namespace, labels: pod.Metadata.Labels})
			for _, ref := range podReferences(pod.Spec) {
				references = append(references, referenceInfo{
					from: id, namespace: namespace, kind: ref.kind, name: ref.name,
				})
			}

		case "Service":
			var serviceDoc serviceDocument
			if err := root.Decode(&serviceDoc); err != nil {
				return nil, fmt.Errorf("kubernetes: %s:%d: Service %q: %w", path, line, name, err)
			}
			serviceType := serviceDoc.Spec.Type
			if serviceType == "" {
				serviceType = "ClusterIP"
			}
			nodeType = model.NodeService
			if serviceType == "LoadBalancer" {
				nodeType = model.NodeLoadBalancer
			}
			metadata["type"] = serviceType
			if ports := servicePorts(serviceDoc.Spec.Ports); len(ports) > 0 {
				metadata["ports"] = strings.Join(ports, ", ")
			}
			if len(serviceDoc.Spec.Selector) > 0 {
				metadata["selector"] = formatLabels(serviceDoc.Spec.Selector)
			}
			services = append(services, serviceInfo{
				id: id, namespace: namespace, selector: serviceDoc.Spec.Selector,
			})

		case "Ingress":
			var ingressDoc ingressDocument
			if err := root.Decode(&ingressDoc); err != nil {
				return nil, fmt.Errorf("kubernetes: %s:%d: Ingress %q: %w", path, line, name, err)
			}
			nodeType = model.NodeIngress
			hosts := ingressHosts(ingressDoc)
			if len(hosts) > 0 {
				metadata["hosts"] = strings.Join(hosts, ", ")
			}
			for _, serviceName := range ingressServices(ingressDoc) {
				ingressTargets = append(ingressTargets, ingressInfo{
					from: id, namespace: namespace, service: serviceName,
				})
			}

		case "PersistentVolumeClaim":
			var claim pvcDocument
			if err := root.Decode(&claim); err != nil {
				return nil, fmt.Errorf("kubernetes: %s:%d: PersistentVolumeClaim %q: %w", path, line, name, err)
			}
			nodeType = model.NodeStorage
			if claim.Spec.StorageClassName != "" {
				metadata["storage_class"] = claim.Spec.StorageClassName
			}
			if len(claim.Spec.AccessModes) > 0 {
				metadata["access_modes"] = strings.Join(claim.Spec.AccessModes, ", ")
			}
			if storage := claim.Spec.Resources.Requests["storage"]; storage != "" {
				metadata["storage"] = storage
			}

		case "ConfigMap":
			nodeType = model.NodeOther

		case "Secret":
			var secret struct {
				Type string `yaml:"type"`
			}
			if err := root.Decode(&secret); err != nil {
				return nil, fmt.Errorf("kubernetes: %s:%d: Secret %q: %w", path, line, name, err)
			}
			nodeType = model.NodeOther
			if secret.Type != "" {
				metadata["type"] = secret.Type
			}

		case "ServiceAccount":
			nodeType = model.NodeOther

		case "HorizontalPodAutoscaler":
			var hpa hpaDocument
			if err := root.Decode(&hpa); err != nil {
				return nil, fmt.Errorf("kubernetes: %s:%d: HorizontalPodAutoscaler %q: %w", path, line, name, err)
			}
			nodeType = model.NodeOther
			if hpa.Spec.MinReplicas != nil {
				metadata["min_replicas"] = fmt.Sprintf("%d", *hpa.Spec.MinReplicas)
			}
			if hpa.Spec.MaxReplicas > 0 {
				metadata["max_replicas"] = fmt.Sprintf("%d", hpa.Spec.MaxReplicas)
			}
			if hpa.Spec.ScaleTargetRef.Kind != "" && hpa.Spec.ScaleTargetRef.Name != "" {
				metadata["target"] = hpa.Spec.ScaleTargetRef.Kind + "/" + hpa.Spec.ScaleTargetRef.Name
				scaleTargets = append(scaleTargets, scaleTargetInfo{
					from: id, namespace: namespace,
					kind: hpa.Spec.ScaleTargetRef.Kind, name: hpa.Spec.ScaleTargetRef.Name,
				})
			}

		case "PodDisruptionBudget":
			nodeType = model.NodeOther
			metadata["kind"] = header.Kind

		case "NetworkPolicy":
			nodeType = model.NodeNetwork
			metadata["kind"] = header.Kind

		case "Namespace", "CustomResourceDefinition":
			nodeType = model.NodeOther
			metadata["kind"] = header.Kind

		default:
			// Unknown CRDs and cluster extensions are kept as generic nodes
			// instead of being silently dropped.
			nodeType = model.NodeOther
			metadata["kind"] = header.Kind
			metadata["api_version"] = header.APIVersion
		}

		objects[id] = true
		graph.Nodes = append(graph.Nodes, model.Node{
			ID:       id,
			Name:     name,
			Type:     nodeType,
			Group:    groupID,
			Source:   model.Source{File: path, Line: line},
			Metadata: redact.Metadata(metadata),
			Status:   model.StatusUnchanged,
		})
	}

	if supported == 0 {
		return nil, fmt.Errorf("kubernetes: %s: no Kubernetes objects found; add apiVersion, kind, and metadata.name", strings.Join(paths, ", "))
	}

	graph.Edges = buildEdges(objects, workloads, services, references, ingressTargets, scaleTargets)

	graph.Sort()
	if err := graph.Validate(); err != nil {
		return nil, err
	}
	return graph, nil
}

func addPodTemplateMetadata(metadata map[string]string, template podTemplate) {
	images := containerImages(template.Spec)
	if len(images) > 0 {
		metadata["images"] = strings.Join(images, ", ")
	}
}

type workloadInfo struct {
	id        string
	namespace string
	labels    map[string]string
}

type serviceInfo struct {
	id        string
	namespace string
	selector  map[string]string
}

type referenceInfo struct {
	from      string
	namespace string
	kind      string
	name      string
}

type ingressInfo struct {
	from      string
	namespace string
	service   string
}

type scaleTargetInfo struct {
	from      string
	namespace string
	kind      string
	name      string
}

func buildEdges(
	objects map[string]bool,
	workloads []workloadInfo,
	services []serviceInfo,
	references []referenceInfo,
	ingressTargets []ingressInfo,
	scaleTargets []scaleTargetInfo,
) []model.Edge {
	var edges []model.Edge
	seen := make(map[string]bool)
	add := func(from, to string, kind model.EdgeKind) {
		if from == to || !objects[from] || !objects[to] {
			return
		}
		key := from + "->" + to
		if seen[key] {
			return
		}
		seen[key] = true
		edges = append(edges, model.Edge{
			From: from, To: to, Kind: kind, Status: model.StatusUnchanged,
		})
	}

	for _, service := range services {
		for _, workload := range workloads {
			if service.namespace != workload.namespace {
				continue
			}
			if labelsMatch(service.selector, workload.labels) {
				add(service.id, workload.id, model.EdgeRoutesTo)
			}
		}
	}
	for _, target := range ingressTargets {
		add(target.from, objectID("Service", target.namespace, target.service), model.EdgeRoutesTo)
	}
	for _, target := range scaleTargets {
		add(target.from, objectID(target.kind, target.namespace, target.name), model.EdgeReference)
	}
	for _, reference := range references {
		kind := model.EdgeReference
		if reference.kind == "PersistentVolumeClaim" {
			kind = model.EdgeVolume
		}
		add(reference.from, objectID(reference.kind, reference.namespace, reference.name), kind)
	}
	return edges
}

func objectID(kind, namespace, name string) string {
	return "k8s:" + strings.ToLower(kind) + ":" + namespace + "/" + name
}

func objectIDFor(kind, namespace, name string) string {
	if namespace == "" {
		return "k8s:" + strings.ToLower(kind) + ":" + name
	}
	return objectID(kind, namespace, name)
}

var clusterScopedKinds = map[string]bool{
	"Namespace":                      true,
	"Node":                           true,
	"PersistentVolume":               true,
	"ClusterRole":                    true,
	"ClusterRoleBinding":             true,
	"CustomResourceDefinition":       true,
	"StorageClass":                   true,
	"PriorityClass":                  true,
	"IngressClass":                   true,
	"RuntimeClass":                   true,
	"CSIDriver":                      true,
	"CSINode":                        true,
	"VolumeAttachment":               true,
	"APIService":                     true,
	"MutatingWebhookConfiguration":   true,
	"ValidatingWebhookConfiguration": true,
}

var knownNamespaced = map[string]bool{
	"Deployment":              true,
	"StatefulSet":             true,
	"DaemonSet":               true,
	"ReplicaSet":              true,
	"Job":                     true,
	"CronJob":                 true,
	"Pod":                     true,
	"Service":                 true,
	"Ingress":                 true,
	"PersistentVolumeClaim":   true,
	"ConfigMap":               true,
	"Secret":                  true,
	"ServiceAccount":          true,
	"HorizontalPodAutoscaler": true,
	"PodDisruptionBudget":     true,
	"NetworkPolicy":           true,
}

func isClusterScoped(kind string) bool {
	return clusterScopedKinds[kind]
}

type objectMeta struct {
	Name      string            `yaml:"name"`
	Namespace string            `yaml:"namespace"`
	Labels    map[string]string `yaml:"labels"`
}

type header struct {
	APIVersion string     `yaml:"apiVersion"`
	Kind       string     `yaml:"kind"`
	Metadata   objectMeta `yaml:"metadata"`
}

type nameRef struct {
	Name string `yaml:"name"`
}

type podSpec struct {
	Containers []struct {
		Name    string    `yaml:"name"`
		Image   string    `yaml:"image"`
		Env     []envVar  `yaml:"env"`
		EnvFrom []envFrom `yaml:"envFrom"`
	}
	Volumes          []volume  `yaml:"volumes"`
	ImagePullSecrets []nameRef `yaml:"imagePullSecrets"`
}

type envVar struct {
	Name      string `yaml:"name"`
	ValueFrom struct {
		SecretKeyRef    *nameRef `yaml:"secretKeyRef"`
		ConfigMapKeyRef *nameRef `yaml:"configMapKeyRef"`
	} `yaml:"valueFrom"`
}

type envFrom struct {
	SecretRef    *nameRef `yaml:"secretRef"`
	ConfigMapRef *nameRef `yaml:"configMapRef"`
}

type volume struct {
	Name      string   `yaml:"name"`
	ConfigMap *nameRef `yaml:"configMap"`
	Secret    *struct {
		SecretName string `yaml:"secretName"`
	} `yaml:"secret"`
	PersistentVolumeClaim *struct {
		ClaimName string `yaml:"claimName"`
	} `yaml:"persistentVolumeClaim"`
}

type podTemplate struct {
	Metadata objectMeta `yaml:"metadata"`
	Spec     podSpec    `yaml:"spec"`
}

type workloadDocument struct {
	Metadata objectMeta `yaml:"metadata"`
	Spec     struct {
		Replicas *int `yaml:"replicas"`
		Selector struct {
			MatchLabels map[string]string `yaml:"matchLabels"`
		} `yaml:"selector"`
		Template podTemplate `yaml:"template"`
	} `yaml:"spec"`
}

type jobDocument struct {
	Metadata objectMeta `yaml:"metadata"`
	Spec     struct {
		Parallelism *int        `yaml:"parallelism"`
		Completions *int        `yaml:"completions"`
		Template    podTemplate `yaml:"template"`
	} `yaml:"spec"`
}

type cronJobDocument struct {
	Metadata objectMeta `yaml:"metadata"`
	Spec     struct {
		Schedule    string `yaml:"schedule"`
		JobTemplate struct {
			Spec struct {
				Template podTemplate `yaml:"template"`
			} `yaml:"spec"`
		} `yaml:"jobTemplate"`
	} `yaml:"spec"`
}

type podDocument struct {
	Metadata objectMeta `yaml:"metadata"`
	Spec     podSpec    `yaml:"spec"`
}

type hpaDocument struct {
	Metadata objectMeta `yaml:"metadata"`
	Spec     struct {
		MinReplicas    *int `yaml:"minReplicas"`
		MaxReplicas    int  `yaml:"maxReplicas"`
		ScaleTargetRef struct {
			APIVersion string `yaml:"apiVersion"`
			Kind       string `yaml:"kind"`
			Name       string `yaml:"name"`
		} `yaml:"scaleTargetRef"`
	} `yaml:"spec"`
}

type serviceDocument struct {
	Metadata objectMeta `yaml:"metadata"`
	Spec     struct {
		Type     string            `yaml:"type"`
		Selector map[string]string `yaml:"selector"`
		Ports    []struct {
			Name     string `yaml:"name"`
			Port     int    `yaml:"port"`
			Protocol string `yaml:"protocol"`
		} `yaml:"ports"`
	} `yaml:"spec"`
}

type ingressDocument struct {
	Metadata objectMeta `yaml:"metadata"`
	Spec     struct {
		DefaultBackend *ingressBackend `yaml:"defaultBackend"`
		Rules          []struct {
			Host string `yaml:"host"`
			HTTP struct {
				Paths []struct {
					Backend ingressBackend `yaml:"backend"`
				} `yaml:"paths"`
			} `yaml:"http"`
		} `yaml:"rules"`
		TLS []struct {
			Hosts []string `yaml:"hosts"`
		} `yaml:"tls"`
	} `yaml:"spec"`
}

type ingressBackend struct {
	Service     *struct{ Name string } `yaml:"service"`
	ServiceName string                 `yaml:"serviceName"`
}

type pvcDocument struct {
	Metadata objectMeta `yaml:"metadata"`
	Spec     struct {
		StorageClassName string   `yaml:"storageClassName"`
		AccessModes      []string `yaml:"accessModes"`
		Resources        struct {
			Requests map[string]string `yaml:"requests"`
		} `yaml:"resources"`
	} `yaml:"spec"`
}

type documentSource struct {
	path string
	node *yaml.Node
}

func readDocumentSources(paths []string) ([]documentSource, error) {
	var sources []documentSource
	for _, path := range paths {
		documents, err := readDocuments(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		for _, document := range documents {
			expandDocument(path, document, &sources)
		}
	}
	return sources, nil
}

// expandDocument flattens kind: List documents recursively so their items are
// treated like ordinary manifests, including nested Lists.
func expandDocument(path string, document *yaml.Node, sources *[]documentSource) {
	root := documentRoot(document)
	if root == nil {
		return
	}
	if yamlScalar(root, "kind") == "List" {
		items := yamlMappingValue(root, "items")
		if items != nil && items.Kind == yaml.SequenceNode {
			for _, item := range items.Content {
				expandDocument(path, item, sources)
			}
		}
		return
	}
	*sources = append(*sources, documentSource{path: path, node: document})
}

func yamlMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func yamlScalar(node *yaml.Node, key string) string {
	value := yamlMappingValue(node, key)
	if value == nil || value.Kind != yaml.ScalarNode {
		return ""
	}
	return value.Value
}

func readDocuments(path string) ([]*yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var documents []*yaml.Node
	for {
		var document yaml.Node
		err := decoder.Decode(&document)
		if err == io.EOF {
			return documents, nil
		}
		if err != nil {
			return nil, err
		}
		if document.Kind == 0 {
			continue
		}
		documents = append(documents, &document)
	}
}

func documentRoot(document *yaml.Node) *yaml.Node {
	if document == nil {
		return nil
	}
	if document.Kind == yaml.DocumentNode {
		if len(document.Content) == 0 {
			return nil
		}
		return document.Content[0]
	}
	if document.Kind == yaml.MappingNode {
		return document
	}
	return nil
}

func decodeHeader(root *yaml.Node) (header, error) {
	var decoded header
	if err := root.Decode(&decoded); err != nil {
		return header{}, err
	}
	return decoded, nil
}

func mergeLabels(maps ...map[string]string) map[string]string {
	merged := make(map[string]string)
	for _, labels := range maps {
		for key, value := range labels {
			merged[key] = value
		}
	}
	return merged
}

func labelsMatch(selector, labels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for key, value := range selector {
		if labels[key] != value {
			return false
		}
	}
	return true
}

func formatLabels(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+labels[key])
	}
	return strings.Join(parts, ", ")
}

func containerImages(spec podSpec) []string {
	set := make(map[string]bool)
	for _, container := range spec.Containers {
		if container.Image != "" {
			set[container.Image] = true
		}
	}
	images := make([]string, 0, len(set))
	for image := range set {
		images = append(images, image)
	}
	sort.Strings(images)
	return images
}

func servicePorts(ports []struct {
	Name     string `yaml:"name"`
	Port     int    `yaml:"port"`
	Protocol string `yaml:"protocol"`
}) []string {
	out := make([]string, 0, len(ports))
	for _, port := range ports {
		protocol := port.Protocol
		if protocol == "" {
			protocol = "TCP"
		}
		name := ""
		if port.Name != "" {
			name = port.Name + ":"
		}
		out = append(out, fmt.Sprintf("%s%d/%s", name, port.Port, protocol))
	}
	sort.Strings(out)
	return out
}

type podReference struct {
	kind string
	name string
}

func podReferences(spec podSpec) []podReference {
	var refs []podReference
	add := func(kind, name string) {
		if name != "" {
			refs = append(refs, podReference{kind: kind, name: name})
		}
	}
	for _, container := range spec.Containers {
		for _, env := range container.Env {
			if env.ValueFrom.SecretKeyRef != nil {
				add("Secret", env.ValueFrom.SecretKeyRef.Name)
			}
			if env.ValueFrom.ConfigMapKeyRef != nil {
				add("ConfigMap", env.ValueFrom.ConfigMapKeyRef.Name)
			}
		}
		for _, from := range container.EnvFrom {
			if from.SecretRef != nil {
				add("Secret", from.SecretRef.Name)
			}
			if from.ConfigMapRef != nil {
				add("ConfigMap", from.ConfigMapRef.Name)
			}
		}
	}
	for _, volume := range spec.Volumes {
		if volume.ConfigMap != nil {
			add("ConfigMap", volume.ConfigMap.Name)
		}
		if volume.Secret != nil {
			add("Secret", volume.Secret.SecretName)
		}
		if volume.PersistentVolumeClaim != nil {
			add("PersistentVolumeClaim", volume.PersistentVolumeClaim.ClaimName)
		}
	}
	for _, pullSecret := range spec.ImagePullSecrets {
		add("Secret", pullSecret.Name)
	}
	return refs
}

func ingressHosts(doc ingressDocument) []string {
	set := make(map[string]bool)
	for _, rule := range doc.Spec.Rules {
		if rule.Host != "" {
			set[rule.Host] = true
		}
	}
	for _, tls := range doc.Spec.TLS {
		for _, host := range tls.Hosts {
			if host != "" {
				set[host] = true
			}
		}
	}
	hosts := make([]string, 0, len(set))
	for host := range set {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	return hosts
}

func ingressServices(doc ingressDocument) []string {
	set := make(map[string]bool)
	collect := func(backend ingressBackend) {
		if backend.Service != nil && backend.Service.Name != "" {
			set[backend.Service.Name] = true
		}
		if backend.ServiceName != "" {
			set[backend.ServiceName] = true
		}
	}
	if doc.Spec.DefaultBackend != nil {
		collect(*doc.Spec.DefaultBackend)
	}
	for _, rule := range doc.Spec.Rules {
		for _, path := range rule.HTTP.Paths {
			collect(path.Backend)
		}
	}
	services := make([]string, 0, len(set))
	for name := range set {
		services = append(services, name)
	}
	sort.Strings(services)
	return services
}
