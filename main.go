package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type vErr struct {
	line int
	msg  string
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stdout, "usage: yamlvalid <file.yaml>")
		os.Exit(2)
	}
	filename := os.Args[1]
	b, err := os.ReadFile(filename)
	if err != nil {
		fmt.Fprintf(os.Stdout, "%s: cannot read file content: %v\n", filename, err)
		os.Exit(1)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(b, &root); err != nil {
		fmt.Fprintf(os.Stdout, "%s: cannot unmarshal file content: %v\n", filename, err)
		os.Exit(1)
	}
	errs := validate(&root)
	if len(errs) > 0 {
		for _, e := range errs {
			if e.line > 0 {
				fmt.Fprintf(os.Stdout, "%s:%d %s\n", filename, e.line, e.msg)
			} else {
				fmt.Fprintln(os.Stdout, e.msg)
			}
		}
		os.Exit(1)
	}
}

func validate(root *yaml.Node) []vErr {
	var errs []vErr
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return requiredTopLevelErrors()
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return requiredTopLevelErrors()
	}
	apiVersion, okAPIVersion := mapGet(doc, "apiVersion")
	kind, okKind := mapGet(doc, "kind")
	metadata, okMetadata := mapGet(doc, "metadata")
	spec, okSpec := mapGet(doc, "spec")
	if !okAPIVersion {
		errs = append(errs, vErr{msg: "apiVersion is required"})
	} else {
		if !isString(apiVersion) {
			errs = append(errs, vErr{line: apiVersion.Line, msg: "apiVersion must be string"})
		} else if apiVersion.Value != "v1" {
			errs = append(errs, vErr{line: apiVersion.Line, msg: "apiVersion has unsupported value '" + apiVersion.Value + "'"})
		}
	}
	if !okKind {
		errs = append(errs, vErr{msg: "kind is required"})
	} else {
		if !isString(kind) {
			errs = append(errs, vErr{line: kind.Line, msg: "kind must be string"})
		} else if kind.Value != "Pod" {
			errs = append(errs, vErr{line: kind.Line, msg: "kind has unsupported value '" + kind.Value + "'"})
		}
	}
	if !okMetadata {
		errs = append(errs, vErr{msg: "metadata is required"})
	} else {
		validateMetadata(metadata, &errs)
	}
	if !okSpec {
		errs = append(errs, vErr{msg: "spec is required"})
	} else {
		validateSpec(spec, &errs)
	}
	return errs
}

func requiredTopLevelErrors() []vErr {
	return []vErr{
		{msg: "apiVersion is required"},
		{msg: "kind is required"},
		{msg: "metadata is required"},
		{msg: "spec is required"},
	}
}

func validateMetadata(n *yaml.Node, errs *[]vErr) {
	if n.Kind != yaml.MappingNode {
		*errs = append(*errs, vErr{line: n.Line, msg: "metadata must be object"})
		return
	}
	name, okName := mapGet(n, "name")
	if !okName {
		*errs = append(*errs, vErr{msg: "metadata.name is required"})
	} else if !isString(name) {
		*errs = append(*errs, vErr{line: name.Line, msg: "metadata.name must be string"})
	} else if name.Value == "" {
		*errs = append(*errs, vErr{line: name.Line, msg: "name is required"})
	}
	if ns, ok := mapGet(n, "namespace"); ok {
		if !isString(ns) {
			*errs = append(*errs, vErr{line: ns.Line, msg: "metadata.namespace must be string"})
		}
	}
	if labels, ok := mapGet(n, "labels"); ok {
		if labels.Kind != yaml.MappingNode {
			*errs = append(*errs, vErr{line: labels.Line, msg: "labels must be object"})
		} else {
			for i := 0; i+1 < len(labels.Content); i += 2 {
				keyNode := labels.Content[i]
				valNode := labels.Content[i+1]
				key := keyNode.Value
				if !isString(valNode) {
					*errs = append(*errs, vErr{line: valNode.Line, msg: "labels." + key + " must be string"})
				}
			}
		}
	}
}

func validateSpec(n *yaml.Node, errs *[]vErr) {
	if n.Kind != yaml.MappingNode {
		*errs = append(*errs, vErr{line: n.Line, msg: "spec must be object"})
		return
	}
	if osNode, ok := mapGet(n, "os"); ok {
		if !isString(osNode) {
			*errs = append(*errs, vErr{line: osNode.Line, msg: "os must be string"})
		} else if osNode.Value != "linux" && osNode.Value != "windows" {
			*errs = append(*errs, vErr{line: osNode.Line, msg: "os has unsupported value '" + osNode.Value + "'"})
		}
	}
	containers, ok := mapGet(n, "containers")
	if !ok {
		*errs = append(*errs, vErr{msg: "spec.containers is required"})
		return
	}
	if containers.Kind != yaml.SequenceNode {
		*errs = append(*errs, vErr{line: containers.Line, msg: "spec.containers must be array"})
		return
	}
	for _, item := range containers.Content {
		if item.Kind != yaml.MappingNode {
			*errs = append(*errs, vErr{line: item.Line, msg: "containers must be object"})
			continue
		}
		validateContainer(item, errs)
	}
}

var (
	reSnake    = regexp.MustCompile(`^[a-z0-9]+(?:_[a-z0-9]+)*$`)
	reImage    = regexp.MustCompile(`^registry\.bigbrother\.io/[^\s:@]+:[^\s]+$`)
	reMemUnits = regexp.MustCompile(`^\d+(Gi|Mi|Ki)$`)
)

func validateContainer(n *yaml.Node, errs *[]vErr) {
	if name, ok := mapGet(n, "name"); !ok {
		*errs = append(*errs, vErr{msg: "containers.name is required"})
	} else if !isString(name) {
		*errs = append(*errs, vErr{line: name.Line, msg: "containers.name must be string"})
	} else if name.Value == "" {
		*errs = append(*errs, vErr{line: name.Line, msg: "name is required"})
	} else if !reSnake.MatchString(name.Value) {
		*errs = append(*errs, vErr{line: name.Line, msg: "containers.name has invalid format '" + name.Value + "'"})
	}
	if img, ok := mapGet(n, "image"); !ok {
		*errs = append(*errs, vErr{msg: "containers.image is required"})
	} else if !isString(img) {
		*errs = append(*errs, vErr{line: img.Line, msg: "containers.image must be string"})
	} else if img.Value == "" {
		*errs = append(*errs, vErr{line: img.Line, msg: "image is required"})
	} else if !reImage.MatchString(img.Value) {
		*errs = append(*errs, vErr{line: img.Line, msg: "containers.image has invalid format '" + img.Value + "'"})
	}
	if ports, ok := mapGet(n, "ports"); ok {
		if ports.Kind != yaml.SequenceNode {
			*errs = append(*errs, vErr{line: ports.Line, msg: "containers.ports must be array"})
		} else {
			for _, p := range ports.Content {
				if p.Kind != yaml.MappingNode {
					*errs = append(*errs, vErr{line: p.Line, msg: "ports must be object"})
					continue
				}
				validateContainerPort(p, errs)
			}
		}
	}
	if r, ok := mapGet(n, "readinessProbe"); ok {
		if r.Kind != yaml.MappingNode {
			*errs = append(*errs, vErr{line: r.Line, msg: "readinessProbe must be object"})
		} else {
			validateProbe("readinessProbe", r, errs)
		}
	}
	if l, ok := mapGet(n, "livenessProbe"); ok {
		if l.Kind != yaml.MappingNode {
			*errs = append(*errs, vErr{line: l.Line, msg: "livenessProbe must be object"})
		} else {
			validateProbe("livenessProbe", l, errs)
		}
	}
	res, ok := mapGet(n, "resources")
	if !ok {
		*errs = append(*errs, vErr{msg: "resources is required"})
		return
	}
	if res.Kind != yaml.MappingNode {
		*errs = append(*errs, vErr{line: res.Line, msg: "resources must be object"})
		return
	}
	validateResources(res, errs)
}

func validateContainerPort(n *yaml.Node, errs *[]vErr) {
	if cp, ok := mapGet(n, "containerPort"); !ok {
		*errs = append(*errs, vErr{msg: "containerPort is required"})
	} else if !isInt(cp) {
		*errs = append(*errs, vErr{line: cp.Line, msg: "containerPort must be int"})
	} else if !portInRange(cp.Value) {
		*errs = append(*errs, vErr{line: cp.Line, msg: "containerPort value out of range"})
	}
	if proto, ok := mapGet(n, "protocol"); ok {
		if !isString(proto) {
			*errs = append(*errs, vErr{line: proto.Line, msg: "protocol must be string"})
		} else if proto.Value != "TCP" && proto.Value != "UDP" {
			*errs = append(*errs, vErr{line: proto.Line, msg: "protocol has unsupported value '" + proto.Value + "'"})
		}
	}
}

func validateProbe(prefix string, n *yaml.Node, errs *[]vErr) {
	httpGet, ok := mapGet(n, "httpGet")
	if !ok {
		*errs = append(*errs, vErr{msg: prefix + ".httpGet is required"})
		return
	}
	if httpGet.Kind != yaml.MappingNode {
		*errs = append(*errs, vErr{line: httpGet.Line, msg: prefix + ".httpGet must be object"})
		return
	}
	if p, ok := mapGet(httpGet, "path"); !ok {
		*errs = append(*errs, vErr{msg: "path is required"})
	} else if !isString(p) {
		*errs = append(*errs, vErr{line: p.Line, msg: "path must be string"})
	} else if p.Value == "" || !strings.HasPrefix(p.Value, "/") {
		*errs = append(*errs, vErr{line: p.Line, msg: "path has invalid format '" + p.Value + "'"})
	}
	if port, ok := mapGet(httpGet, "port"); !ok {
		*errs = append(*errs, vErr{msg: "port is required"})
	} else if !isInt(port) {
		*errs = append(*errs, vErr{line: port.Line, msg: "port must be int"})
	} else if !portInRange(port.Value) {
		*errs = append(*errs, vErr{line: port.Line, msg: "port value out of range"})
	}
}

func validateResources(n *yaml.Node, errs *[]vErr) {
	if limits, ok := mapGet(n, "limits"); ok {
		if limits.Kind != yaml.MappingNode {
			*errs = append(*errs, vErr{line: limits.Line, msg: "limits must be object"})
		} else {
			validateResourceScope(limits, errs)
		}
	}
	if req, ok := mapGet(n, "requests"); ok {
		if req.Kind != yaml.MappingNode {
			*errs = append(*errs, vErr{line: req.Line, msg: "requests must be object"})
		} else {
			validateResourceScope(req, errs)
		}
	}
}

func validateResourceScope(n *yaml.Node, errs *[]vErr) {
	if cpu, ok := mapGet(n, "cpu"); ok {
		if !isInt(cpu) {
			*errs = append(*errs, vErr{line: cpu.Line, msg: "cpu must be int"})
		}
	}
	if mem, ok := mapGet(n, "memory"); ok {
		if !isString(mem) {
			*errs = append(*errs, vErr{line: mem.Line, msg: "memory must be string"})
		} else if !reMemUnits.MatchString(mem.Value) {
			*errs = append(*errs, vErr{line: mem.Line, msg: "memory has invalid format '" + mem.Value + "'"})
		}
	}
}

func isString(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.Tag == "!!str"
}

func isInt(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.Tag == "!!int"
}

func portInRange(s string) bool {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return false
	}
	return v > 0 && v < 65536
}

func mapGet(m *yaml.Node, key string) (*yaml.Node, bool) {
	if m.Kind != yaml.MappingNode {
		return nil, false
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		k := m.Content[i]
		v := m.Content[i+1]
		if k.Kind == yaml.ScalarNode && k.Value == key {
			return v, true
		}
	}
	return nil, false
}
