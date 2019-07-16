// Copyright 2018 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gengapic

import (
	"path/filepath"
	"testing"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/protoc-gen-go/descriptor"
	"github.com/golang/protobuf/ptypes/duration"
	conf "github.com/googleapis/gapic-generator-go/internal/grpc_service_config"
	"github.com/googleapis/gapic-generator-go/internal/pbinfo"
	"github.com/googleapis/gapic-generator-go/internal/txtdiff"
	"google.golang.org/genproto/googleapis/api/annotations"
	code "google.golang.org/genproto/googleapis/rpc/code"
)

func TestClientOpt(t *testing.T) {
	var g generator
	g.imports = map[pbinfo.ImportSpec]bool{}
	g.grpcConf = &conf.ServiceConfig{
		MethodConfig: []*conf.MethodConfig{
			&conf.MethodConfig{
				Name: []*conf.MethodConfig_Name{
					&conf.MethodConfig_Name{
						Service: "bar.FooService",
						Method:  "Zip",
					},
				},
				RetryOrHedgingPolicy: &conf.MethodConfig_RetryPolicy_{
					RetryPolicy: &conf.MethodConfig_RetryPolicy{
						InitialBackoff:    &duration.Duration{Nanos: 100000000},
						MaxBackoff:        &duration.Duration{Seconds: 60},
						BackoffMultiplier: 1.3,
						RetryableStatusCodes: []code.Code{
							code.Code_UNKNOWN,
						},
					},
				},
			},
			&conf.MethodConfig{
				Name: []*conf.MethodConfig_Name{
					&conf.MethodConfig_Name{
						Service: "bar.FooService",
					},
				},
				RetryOrHedgingPolicy: &conf.MethodConfig_RetryPolicy_{
					RetryPolicy: &conf.MethodConfig_RetryPolicy{
						InitialBackoff:    &duration.Duration{Nanos: 10000000},
						MaxBackoff:        &duration.Duration{Seconds: 7},
						BackoffMultiplier: 1.1,
						RetryableStatusCodes: []code.Code{
							code.Code_UNKNOWN,
						},
					},
				},
			},
		},
	}

	serv := &descriptor.ServiceDescriptorProto{
		Name: proto.String("FooService"),
		Method: []*descriptor.MethodDescriptorProto{
			{Name: proto.String("Zip"), Options: &descriptor.MethodOptions{}},
			{Name: proto.String("Zap"), Options: &descriptor.MethodOptions{}},
			{Name: proto.String("Smack")},
		},
		Options: &descriptor.ServiceOptions{},
	}
	if err := proto.SetExtension(serv.Options, annotations.E_DefaultHost, proto.String("foo.bar.com")); err != nil {
		t.Fatal(err)
	}

	g.descInfo = pbinfo.Info{
		ParentFile: map[proto.Message]*descriptor.FileDescriptorProto{
			serv: &descriptor.FileDescriptorProto{
				Package: proto.String("bar"),
			},
		},
	}

	// Test some annotations
	if err := proto.SetExtension(serv.Method[0].Options, annotations.E_Http, &annotations.HttpRule{
		Pattern: &annotations.HttpRule_Get{
			Get: "/zip",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := proto.SetExtension(serv.Method[1].Options, annotations.E_Http, &annotations.HttpRule{
		Pattern: &annotations.HttpRule_Post{
			Post: "/zap",
		},
	}); err != nil {
		t.Fatal(err)
	}

	servHostPort := &descriptor.ServiceDescriptorProto{
		Method: []*descriptor.MethodDescriptorProto{
			{Name: proto.String("Smack")},
		},
		Options: &descriptor.ServiceOptions{},
	}
	if err := proto.SetExtension(servHostPort.Options, annotations.E_DefaultHost, proto.String("foo.bar.com:1234")); err != nil {
		t.Fatal(err)
	}

	for _, tst := range []struct {
		tstName, servName string
		serv              *descriptor.ServiceDescriptorProto
	}{
		{tstName: "foo_opt", servName: "Foo", serv: serv},
		{tstName: "empty_opt", servName: "", serv: serv},
		{tstName: "host_port_opt", servName: "Bar", serv: servHostPort},
	} {
		g.reset()
		if err := g.clientOptions(tst.serv, tst.servName); err != nil {
			t.Error(err)
			continue
		}
		txtdiff.Diff(t, tst.tstName, g.pt.String(), filepath.Join("testdata", tst.tstName+".want"))
	}
}

func TestClientInit(t *testing.T) {
	var g generator
	g.apiName = "Awesome Foo API"
	g.imports = map[pbinfo.ImportSpec]bool{}

	servPlain := &descriptor.ServiceDescriptorProto{
		Name: proto.String("Foo"),
		Method: []*descriptor.MethodDescriptorProto{
			{Name: proto.String("Zip"), OutputType: proto.String("Foo")},
		},
	}
	servLRO := &descriptor.ServiceDescriptorProto{
		Name: proto.String("Foo"),
		Method: []*descriptor.MethodDescriptorProto{
			{Name: proto.String("Zip"), OutputType: proto.String(".google.longrunning.Operation")},
		},
	}

	for _, tst := range []struct {
		tstName string

		servName string
		serv     *descriptor.ServiceDescriptorProto
	}{
		{tstName: "foo_client_init", servName: "Foo", serv: servPlain},
		{tstName: "empty_client_init", servName: "", serv: servPlain},
		{tstName: "lro_client_init", servName: "Foo", serv: servLRO},
	} {
		g.descInfo.ParentFile = map[proto.Message]*descriptor.FileDescriptorProto{
			tst.serv: &descriptor.FileDescriptorProto{
				Options: &descriptor.FileOptions{
					GoPackage: proto.String("mypackage"),
				},
			},
		}
		g.comments = map[proto.Message]string{
			tst.serv: "Foo service does stuff.",
		}

		g.reset()
		g.clientInit(tst.serv, tst.servName)
		txtdiff.Diff(t, tst.tstName, g.pt.String(), filepath.Join("testdata", tst.tstName+".want"))
	}
}
