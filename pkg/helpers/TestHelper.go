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

package helpers

import (
	"fmt"
	"github.com/vs-eth/microkube/pkg/handlers"
	"github.com/vs-eth/microkube/pkg/pki"
	"io/ioutil"
	"net"
	"time"
)

// UUTConstrutor is implemented by all types that use this simplified mechanism to be tested and is used to create a
// test object with all related resources
type UUTConstrutor func(execEnv handlers.ExecutionEnvironment, creds *pki.MicrokubeCredentials) ([]handlers.ServiceHandler, error)

// StartHandlerForTest starts a given handler for a unit test
func StartHandlerForTest(portbase int, name, binary string, constructor UUTConstrutor, exitHandler handlers.ExitHandler, print bool, healthCheckTries int, credsArg *pki.MicrokubeCredentials, execEnvArg *handlers.ExecutionEnvironment) (handlerList []handlers.ServiceHandler, creds *pki.MicrokubeCredentials, execEnv *handlers.ExecutionEnvironment, err error) {
	tmpdir, err := ioutil.TempDir("", "microkube-unittests-"+name)
	if err != nil {
		return nil, nil, nil, err
	}

	outputHandler := func(output []byte) {
		if print {
			fmt.Println(name+"   |", string(output))
		}
	}

	if credsArg == nil {
		creds = &pki.MicrokubeCredentials{}
		creds.CreateOrLoadCertificates(tmpdir, net.ParseIP("127.0.0.1"), net.ParseIP("127.1.1.1"))
	} else {
		creds = credsArg
	}

	wd, err := FindBinary(binary, "", "")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error while searching for "+name+" binary: '%s'", err)
	}

	execEnv = &handlers.ExecutionEnvironment{
		Binary:        wd,
		ListenAddress: net.ParseIP("127.0.0.1"),
		OutputHandler: outputHandler,
		ExitHandler:   exitHandler,
		Workdir:       tmpdir,
		SudoMethod:    "sudo", // TODO: Make this nicer...
		DNSAddress:    net.ParseIP("8.8.8.8"),
	}
	if execEnvArg == nil {
		execEnv.InitPorts(portbase)
	} else {
		execEnv.CopyInformationFromBase(execEnvArg)
	}

	// UUT
	handlerList, err = constructor(*execEnv, creds)
	if err != nil {
		return nil, nil, nil, fmt.Errorf(name+" handler creation failed: '%s'", err)
	}
	handler := handlerList[len(handlerList)-1]
	err = handler.Start()
	if err != nil {
		return nil, nil, nil, fmt.Errorf(name+" startup failed: '%s'", err)
	}

	healthMessage := make(chan handlers.HealthMessage, 1)
	msg := handlers.HealthMessage{
		IsHealthy: false,
	}
	for i := 0; i < healthCheckTries && (!msg.IsHealthy); i++ {
		handler.EnableHealthChecks(healthMessage, false)
		msg = <-healthMessage
		time.Sleep(1 * time.Second)
	}
	if !msg.IsHealthy {
		return nil, nil, nil, fmt.Errorf(name+" unhealthy: %s", msg.Error)
	}

	return handlerList, creds, execEnv, nil
}
