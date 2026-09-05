package example_test

import (
	"fmt"

	"github.com/yiblet/confy"
)

// Kubernetes-style inline union (KEP-1027): a required string discriminator
// next to optional member fields, validated so that exactly the named member
// is set. This stays on the applicative rung: Optional members + Check, no
// Switch, and the schema is exact.

type VolumeSource struct {
	Type      string // "emptyDir" | "hostPath" | "configMap"
	HostPath  *HostPath
	ConfigMap *ConfigMapRef
}

type HostPath struct{ Path string }
type ConfigMapRef struct {
	Name     string
	Optional bool
}

type Volume struct {
	Name   string
	Source VolumeSource
}

func volumeSourceConfy() confy.Confy[VolumeSource] {
	return confy.Struct[VolumeSource]().
		At(func(v *VolumeSource) *string { return &v.Type },
			confy.Field("type", confy.Enum("emptyDir", "hostPath", "configMap")).Doc("Union discriminator")).
		At(func(v *VolumeSource) **HostPath { return &v.HostPath },
			confy.Optional(confy.Under("hostPath", confy.Struct[HostPath]().
				At(func(h *HostPath) *string { return &h.Path }, confy.String("path"))))).
		At(func(v *VolumeSource) **ConfigMapRef { return &v.ConfigMap },
			confy.Optional(confy.Under("configMap", confy.Struct[ConfigMapRef]().
				At(func(c *ConfigMapRef) *string { return &c.Name }, confy.String("name")).
				At(func(c *ConfigMapRef) *bool { return &c.Optional }, confy.Bool("optional").Default(false))))).
		Check(func(v VolumeSource) error {
			set := 0
			if v.HostPath != nil {
				set++
			}
			if v.ConfigMap != nil {
				set++
			}
			switch v.Type {
			case "emptyDir":
				if set != 0 {
					return fmt.Errorf("type emptyDir takes no member")
				}
			case "hostPath":
				if v.HostPath == nil || set != 1 {
					return fmt.Errorf("type hostPath requires exactly the hostPath member")
				}
			case "configMap":
				if v.ConfigMap == nil || set != 1 {
					return fmt.Errorf("type configMap requires exactly the configMap member")
				}
			}
			return nil
		})
}

func volumesConfy() confy.Confy[[]Volume] {
	return confy.List("volumes", confy.Struct[Volume]().
		At(func(v *Volume) *string { return &v.Name }, confy.String("name")).
		At(func(v *Volume) *VolumeSource { return &v.Source }, volumeSourceConfy()))
}

func Example_kubernetesInlineUnion() {
	vols, err := volumesConfy().Build(parse(`{"volumes": [
	  {"name": "scratch", "type": "emptyDir"},
	  {"name": "logs", "type": "hostPath", "hostPath": {"path": "/var/log"}},
	  {"name": "cfg", "type": "configMap", "configMap": {"name": "app-config"}}
	]}`))
	fmt.Println("err:", err)
	for _, v := range vols {
		switch v.Source.Type {
		case "hostPath":
			fmt.Printf("%s -> hostPath %s\n", v.Name, v.Source.HostPath.Path)
		case "configMap":
			fmt.Printf("%s -> configMap %s (optional=%v)\n", v.Name, v.Source.ConfigMap.Name, v.Source.ConfigMap.Optional)
		default:
			fmt.Printf("%s -> %s\n", v.Name, v.Source.Type)
		}
	}

	// Cross-field violations are reported at the volume, with the path.
	_, err = volumesConfy().Build(parse(`{"volumes": [
	  {"name": "bad1", "type": "emptyDir", "hostPath": {"path": "/x"}},
	  {"name": "bad2", "type": "configMap"},
	  {"name": "bad3", "type": "secret"}
	]}`))
	fmt.Println(err)

	// Output:
	// err: <nil>
	// scratch -> emptyDir
	// logs -> hostPath /var/log
	// cfg -> configMap app-config (optional=false)
	// volumes[0]: type emptyDir takes no member
	// volumes[1]: type configMap requires exactly the configMap member
	// volumes[2].type: expected one of: emptyDir, hostPath, configMap, got "secret": must be one of: emptyDir, hostPath, configMap
}

func Example_kubernetesUnionSchemaIsExact() {
	// No Switch means no over-approximation: every key is listed once, and
	// the members are conditional on their group being present.
	fmt.Print(confy.Docs(volumeSourceConfy().Schema()))
	fmt.Println("---")
	fmt.Print(confy.Template(volumeSourceConfy().Schema()))

	// Output:
	// type                one of: emptyDir, hostPath, configMap  required
	//     Union discriminator
	// hostPath.path       string                                 required       if hostPath is set
	// configMap.name      string                                 required       if configMap is set
	// configMap.optional  bool                                   default false  if configMap is set
	// ---
	// # Union discriminator
	// type: <one of: emptyDir, hostPath, configMap>
	// hostPath:
	//   path: <string>
	// configMap:
	//   name: <string>
	//   optional: false
}
