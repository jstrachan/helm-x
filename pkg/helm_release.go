package x

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/golang/protobuf/ptypes/any"
	"k8s.io/helm/pkg/proto/hapi/chart"
	rspb "k8s.io/helm/pkg/proto/hapi/release"
	"k8s.io/helm/pkg/timeconv"
	"sigs.k8s.io/yaml"
	"strconv"
	"time"
)

type ReleaseManifest func(release *rspb.Release, tillerNs string) (interface{}, error)

func TurnHelmTemplateToInstall(chartName, version, tillerNs, releaseName, ns, manifest string, releaseManifests ...ReleaseManifest) (string, error) {
	man, hooks, err := SplitManifestAndHooks(manifest)
	if err != nil {
		return "", err
	}
	manifestData := []byte(base64.StdEncoding.EncodeToString([]byte(man)))
	vData := []byte(base64.StdEncoding.EncodeToString([]byte("This release is generated by helm-x")))
	templates := []*chart.Template{
		{
			Name: "templates/all.yaml",
			Data: manifestData,
		},
	}
	if version == "" {
		version = "0.0.0"
	}
	c := &chart.Chart{
		Metadata: &chart.Metadata{
			Name:       chartName,
			ApiVersion: "v1",
			AppVersion: version,
			Version:    version,
		},
		Templates:    templates,
		Values:       &chart.Config{Raw: "{}"},
		Dependencies: []*chart.Chart{},
		Files: []*any.Any{
			{
				TypeUrl: "README.md",
				Value:   vData,
			},
		},
	}

	if ns == "" {
		ns = "default"
	}

	ts := timeconv.Now()
	// See `kubectl get configmap -n kube-system -o jsonpath={.data.release} foo.v1 | base64 -D  | gunzip -` for
	// real-world examples
	release := &rspb.Release{
		Name: releaseName,
		Info: &rspb.Info{
			FirstDeployed: ts,
			LastDeployed:  ts,
			Status: &rspb.Status{
				Code: rspb.Status_DEPLOYED,
			},
			Description: fmt.Sprintf("Adopted with helm-x"),
		},
		Chart:    c,
		Config:   &chart.Config{Raw: "{}"},
		Manifest: man,
		Hooks:    hooks,
		// Starts from "1". Try installing any chart and see by running `helm install --name foo yourchart && kubectl -n kube-system get configmap -o yaml foo.v1`
		Version:   1,
		Namespace: ns,
	}

	concatenated := man

	for _, m := range releaseManifests {
		releaseObj, err := m(release, tillerNs)
		if err != nil {
			return "", err
		}

		/// Turn the release object into JSON, and then YAML

		releaseJsonBytes, err := json.Marshal(releaseObj)
		if err != nil {
			return "", err
		}

		releaseYamlBytes, err := yaml.JSONToYAML(releaseJsonBytes)
		if err != nil {
			return "", err
		}

		concatenated = concatenated + "\n---\n" + string(releaseYamlBytes)
	}

	return concatenated, nil
}

func ReleaseToConfigMap(release *rspb.Release, tillerNs string) (interface{}, error) {
	// Adopted from https://github.com/helm/helm/blob/90f50a11db5e81be0edd179b60a50adb9fcf3942/pkg/storage/driver/cfgmaps.go#L152-L164 with love

	var lbs labels

	lbs.init()
	lbs.set("CREATED_AT", strconv.Itoa(int(time.Now().Unix())))

	key := makeKey(release.Name, release.Version)

	cfgmap, err := newConfigMapsObject(key, release, lbs)
	if err != nil {
		return nil, err
	}

	// Can't we automatically set these?
	cfgmap.APIVersion = "v1"
	cfgmap.Kind = "ConfigMap"
	cfgmap.Namespace = tillerNs

	return cfgmap, nil
}

func ReleaseToSecret(release *rspb.Release, tillerNs string) (interface{}, error) {
	// Adopted from https://github.com/helm/helm/blob/90f50a11db5e81be0edd179b60a50adb9fcf3942/pkg/storage/driver/secrets.go#L152-L157 with love

	var lbs labels

	lbs.init()
	lbs.set("CREATED_AT", strconv.Itoa(int(time.Now().Unix())))

	key := makeKey(release.Name, release.Version)

	cfgmap, err := newSecretsObject(key, release, lbs)
	if err != nil {
		return nil, err
	}

	// Can't we automatically set these?
	cfgmap.APIVersion = "v1"
	cfgmap.Kind = "Secret"
	cfgmap.Namespace = tillerNs

	return cfgmap, nil
}
