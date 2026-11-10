# Examples

Small, self-contained inputs for each supported format. The generated Mermaid
and JSON files next to them are produced by the commands below.

## Docker Compose

```bash
infradiff generate examples/compose -f mermaid -o examples/compose/infradiff.mmd
infradiff generate examples/compose -f json -o examples/compose/infradiff.json
```

## Kubernetes

```bash
infradiff generate examples/kubernetes -f mermaid -o examples/kubernetes/infradiff.mmd
infradiff generate examples/kubernetes -f json -o examples/kubernetes/infradiff.json
```

Kubernetes files in one directory are parsed together because workloads
reference ConfigMaps, Secrets, Services, and Ingresses by name.

## Terraform

```bash
infradiff generate examples/terraform -f mermaid -o examples/terraform/infradiff.mmd
infradiff generate examples/terraform -f json -o examples/terraform/infradiff.json
```

Terraform files in one directory are parsed together. A saved plan JSON is also
accepted, for example
`infradiff generate plan.json -f mermaid`.

## HTML

Add `-f html -o infradiff.html` to any command for a self-contained page with
pan and zoom. The page embeds Mermaid and needs no network access.

## Diff

Compare any two directories or files:

```bash
infradiff diff examples/compose examples/compose -f mermaid
```

To compare the working tree against a commit:

```bash
git commit -am "before"
# edit examples/compose/compose.yaml
infradiff diff --git HEAD examples/compose -f mermaid
```
