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

package kube

import (
	"errors"
	"fmt"
	"github.com/vs-eth/microkube/pkg/handlers"
	"github.com/vs-eth/microkube/pkg/helpers"
	"github.com/vs-eth/microkube/pkg/pki"
	"io"
	"io/ioutil"
	"path"
	"strconv"
	"strings"
)

// KubeSchedulerHandler handles invocation of the kubernetes scheduler binary
type KubeSchedulerHandler struct {
	handlers.BaseServiceHandler
	cmd *helpers.CmdHandler

	// Path to kubelet binary
	binary string

	// Path to kubeconfig
	kubeconfig string
	// Path to scheduler config (!= kubeconfig, replacement for commandline flags)
	config string
	// Output handler
	out handlers.OutputHandler
}

// NewKubeSchedulerHandler creates a KubeSchedulerHandler from the arguments provided
func NewKubeSchedulerHandler(execEnv handlers.ExecutionEnvironment, creds *pki.MicrokubeCredentials) (*KubeSchedulerHandler, error) {
	obj := &KubeSchedulerHandler{
		binary:     execEnv.Binary,
		cmd:        nil,
		out:        execEnv.OutputHandler,
		kubeconfig: creds.Kubeconfig,
		config:     path.Join(execEnv.Workdir, "kube-scheduler.cfg"),
	}

	err := CreateKubeSchedulerConfig(obj.config, creds.Kubeconfig, execEnv)
	if err != nil {
		return nil, err
	}

	obj.BaseServiceHandler = *handlers.NewHandler(execEnv.ExitHandler, obj.healthCheckFun, "http://localhost:"+strconv.Itoa(execEnv.KubeSchedulerHealthPort)+"/healthz",
		obj.stop, obj.Start, nil, nil)
	return obj, nil
}

// Stop the child process
func (handler *KubeSchedulerHandler) stop() {
	if handler.cmd != nil {
		handler.cmd.Stop()
	}
}

// Start starts the process, see interface docs
func (handler *KubeSchedulerHandler) Start() error {
	handler.cmd = helpers.NewCmdHandler(handler.binary, []string{
		"kube-scheduler",
		"--config",
		handler.config,
	}, handler.BaseServiceHandler.HandleExit, handler.out, handler.out)
	return handler.cmd.Start()
}

// Handle result of a health probe
func (handler *KubeSchedulerHandler) healthCheckFun(responseBin *io.ReadCloser) error {
	str, err := ioutil.ReadAll(*responseBin)
	if err != nil {
		return err
	}
	if strings.Trim(string(str), " \r\n") != "ok" {
		return errors.New("Health != ok: " + string(str))
	}
	return nil
}

// kubeSchedulerConstructor is supposed to be only used for testing
func kubeSchedulerConstructor(execEnv handlers.ExecutionEnvironment,
	creds *pki.MicrokubeCredentials) ([]handlers.ServiceHandler, error) {

	// Start apiserver (and etcd)
	handlerList, _, _, err := helpers.StartHandlerForTest(-1, "kube-apiserver", "hyperkube",
		kubeApiServerConstructor, execEnv.ExitHandler, false, 30, creds, &execEnv)
	if err != nil {
		return handlerList, fmt.Errorf("kube-apiserver startup prereq failed %s", err)
	}

	// Generate kubeconfig
	tmpdir, err := ioutil.TempDir("", "microkube-unittests-kubeconfig")
	if err != nil {
		return handlerList, fmt.Errorf("tempdir creation failed: %s", err)
	}
	kubeconfig := path.Join(tmpdir, "kubeconfig")
	err = CreateClientKubeconfig(execEnv, creds, kubeconfig, "127.0.0.1")
	if err != nil {
		return handlerList, fmt.Errorf("kubeconfig creation failed: %s", err)
	}

	handler, err := NewKubeSchedulerHandler(execEnv, creds)
	if err != nil {
		return handlerList, fmt.Errorf("kube scheduler handler creation failed: %s", err)
	}
	handlerList = append(handlerList, handler)

	return handlerList, nil
}
