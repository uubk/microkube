/*
 * Copyright 2018 The microkube authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// Package manifests contains the manifest code generator and some pre-supplied manifests (e.g. DNS)
package manifests

import (
	"bytes"
	"io"
	"io/ioutil"
	appsv1 "k8s.io/api/apps/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/client-go/kubernetes/scheme"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// ManifestCodegen is a code generator for generating golang structs from kubernetes manifest files
type ManifestCodegen struct {
	// The manifest to parse
	source string

	currentOutput io.Writer
	// List of entries for the next file
	entries []fileEntry
	// Does the current manifest have something with a health check
	hasHealthCheck bool
	// Name of the type to generate
	name string
	// File to put the type in
	dst string
	// The package to put the result in
	pkg string
	// File to put a main function in, if desired
	mainDest string
	// Package of the main function
	mainPkgBase string
}

func NewManifestCodegen(source, pkg, name, dst, mainDest, mainPkgBase string) *ManifestCodegen {
	return &ManifestCodegen{
		source:      source,
		pkg:         pkg,
		name:        name,
		dst:         dst,
		mainDest:    mainDest,
		mainPkgBase: mainPkgBase,
	}
}

// fileEntry contains information about a single object for inclusion in the next file
type fileEntry struct {
	obj  runtime.Object
	gv   schema.GroupVersion
	name string
}

// ParseFile parses the source file and populates 'entries' in 'm'
func (m *ManifestCodegen) ParseFile() error {
	fileIn, err := os.Open(m.source)
	if err != nil {
		return err
	}
	defer fileIn.Close()

	buf, err := ioutil.ReadAll(fileIn)
	if err != nil {
		return err
	}
	splitRegex := regexp.MustCompilePOSIX(`^\-\-\-`)
	parts := splitRegex.Split(string(buf), -1)
	//parts := strings.Split(string(buf), "---")

	for _, doc := range parts {
		err = m.parseDoc(doc)
		if err != nil {
			return err
		}
	}

	return nil
}

// WriteFiles dumps all previously read information to 'm.dst', optionally writing a main function to 'mainDest'
func (m *ManifestCodegen) WriteFiles() error {
	fd, err := os.OpenFile(m.dst, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)
	defer fd.Close()
	if err != nil {
		return err
	}
	m.currentOutput = fd

	err = m.writeFile()
	if err != nil {
		return err
	}

	if m.mainDest != "" {
		fd, err = os.OpenFile(m.mainDest, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)
		defer fd.Close()
		if err != nil {
			return err
		}
		m.currentOutput = fd
		m.writeMainFile()
	}

	return nil
}

// parseDoc parses a single YAML document, putting the result in 'm.entries'
func (m *ManifestCodegen) parseDoc(doc string) error {
	decodeFun := scheme.Codecs.UniversalDeserializer().Decode
	obj, gvk, err := decodeFun([]byte(doc), nil, nil)
	if err != nil {
		return err
	}

	m.entries = append(m.entries, fileEntry{
		obj:  obj,
		gv:   gvk.GroupVersion(),
		name: "kobjS" + m.name + "O" + strconv.Itoa(len(m.entries)),
	})

	// Check whether this is 'pod generating'
	// 'Pod generating' means that when applying this to a cluster, it will result in a pod being created. This is
	// important for future health checks

	healthObj := fileEntry{
		obj:  obj,
		gv:   gvk.GroupVersion(),
		name: "kobjS" + m.name + "HO",
	}

	if deployment, ok := obj.(*appsv1.Deployment); ok {
		for _, container := range deployment.Spec.Template.Spec.Containers {
			if container.LivenessProbe != nil {
				// Container has health check!
				m.entries = append(m.entries, healthObj)
				m.hasHealthCheck = true
			}
		}
	}

	if deployment, ok := obj.(*extensionsv1beta1.Deployment); ok {
		for _, container := range deployment.Spec.Template.Spec.Containers {
			if container.LivenessProbe != nil {
				// Container has health check!
				m.entries = append(m.entries, healthObj)
				m.hasHealthCheck = true
			}
		}
	}

	return nil
}

// writeMainFile writes a file suitable for building a stand-alone binary with this manifest
func (m *ManifestCodegen) writeMainFile() error {
	bufWriter := bytes.Buffer{}

	bufWriter.WriteString(`/*
 * THIS FILE IS AUTOGENERATED by github.com/vs-eth/microkube/cmd/codegen/Manifest.go
 * DO NOT TOUCH.
 * In case of issues, please fix the code generator ;)
 */

package main

import (
	"flag"
	"github.com/mitchellh/go-homedir"
	log "github.com/sirupsen/logrus"
	"github.com/vs-eth/microkube/internal/cmd"
	"`)
	bufWriter.WriteString(m.mainPkgBase + "/" + m.pkg)
	if m.hasHealthCheck {
		bufWriter.WriteString(`"
	"time`)
	}
	bufWriter.WriteString(`"
)

func main() {
	kubeconfig := flag.String("kubeconfig", "~/.mukube/kube/kubeconfig", "Path to Kubeconfig")
	arg := cmd.ArgHandler{}
	kmri := manifests.KubeManifestRuntimeInfo{
		ExecEnv: *arg.HandleArgs(),
	}
	var err error
	*kubeconfig, err = homedir.Expand(*kubeconfig)
	if err != nil {
		log.WithError(err).WithField("root", *kubeconfig).Fatal("Couldn't expand kubeconfig")
	}
	obj, err := `)
	bufWriter.WriteString(m.pkg + ".New" + m.name)
	bufWriter.WriteString(`(kmri)
	if err != nil {
		log.WithError(err).WithField("root", *kubeconfig).Fatal("Couldn't init object")
	}
	err = obj.ApplyToCluster(*kubeconfig)
	if err != nil {
		log.WithError(err).WithField("root", *kubeconfig).Fatal("Couldn't apply object to cluster")
	}`)
	if m.hasHealthCheck {
		bufWriter.WriteString(`
	err = obj.InitHealthCheck(*kubeconfig)
	if err != nil {
		log.WithError(err).WithField("root", *kubeconfig).Fatal("Couldn't enable health checks")
	}
	ok := false
	for i := 0; i < 10 && !ok; i++ {
		ok, err = obj.IsHealthy()
		if err != nil {
			log.WithError(err).WithField("root", *kubeconfig).Fatal("Couldn't enable health checks")
		}
		if ok {
			break
		}
		time.Sleep(1 * time.Second)
	}
	log.WithField("status", ok).Info("Health check done")`)
	}
	bufWriter.WriteString(`
}
`)
	_, err := bufWriter.WriteTo(m.currentOutput)
	return err
}

// writeFile writes a file containing the generated go struct
func (m *ManifestCodegen) writeFile() error {
	bufWriter := bytes.Buffer{}

	bufWriter.WriteString(`/*
 * THIS FILE IS AUTOGENERATED by github.com/vs-eth/microkube/cmd/codegen/Manifest.go
 * DO NOT TOUCH.
 * In case of issues, please fix the code generator ;)
 */

`)
	bufWriter.WriteString("package " + m.pkg)
	bufWriter.WriteString(`

import (
	"bytes"
	"text/template"
`)
	if m.mainPkgBase+"/"+m.pkg != "github.com/vs-eth/microkube/internal/manifests" {
		bufWriter.WriteString(`
	"github.com/vs-eth/microkube/internal/manifests"
`)
	}
	bufWriter.WriteString(")\n\n")

	serializer := json.Serializer{}
	for _, entry := range m.entries {
		bufWriter.Write([]byte("const " + entry.name + " = `"))

		// Encode the whole thing to JSON
		encoder := scheme.Codecs.EncoderForVersion(&serializer, entry.gv)
		err := encoder.Encode(entry.obj, &bufWriter)
		if err != nil {
			return nil
		}
		// Remove spurious newline
		buf := bufWriter.Bytes()
		if buf[len(buf)-1] == '\n' {
			bufWriter.Truncate(len(buf) - 1)
		}

		bufWriter.Write([]byte("`\n"))
		if err != nil {
			return nil
		}
	}

	m.name = strings.Title(m.name)

	bufWriter.WriteString("\n")
	bufWriter.WriteString("type " + m.name + ` struct {
	`)
	if m.mainPkgBase+"/"+m.pkg != "github.com/vs-eth/microkube/internal/manifests" {
		bufWriter.WriteString("manifests.")
	}
	bufWriter.WriteString(`KubeManifestBase
}

func New` + m.name + `(rtEnv `)
	if m.mainPkgBase+"/"+m.pkg != "github.com/vs-eth/microkube/internal/manifests" {
		bufWriter.WriteString("manifests.")
	}
	bufWriter.WriteString(`KubeManifestRuntimeInfo) (KubeManifest, error) {
	obj := &` + m.name + `{}
	obj.SetName("` + m.name + `")
	var err error
	var buf *bytes.Buffer
	var tmpl *template.Template

`)

	for _, entry := range m.entries {
		// 'HO' means health object, those have to be registered differently
		if !strings.HasSuffix(entry.name, "HO") {
			bufWriter.WriteString(`	tmpl, err = template.New("` + entry.name + `").Parse(` + entry.name + `)
	if err != nil {
		return nil, err
	}
	buf = bytes.NewBufferString("")
	tmpl.Execute(buf, rtEnv)
	obj.Register(buf.String())
`)
		} else {
			bufWriter.WriteString(`	obj.RegisterHO(` + entry.name + ")\n")
		}
	}

	bufWriter.WriteString(`
	return obj, nil
}
`)

	_, err := bufWriter.WriteTo(m.currentOutput)
	return err
}
