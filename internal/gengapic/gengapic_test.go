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
	"github.com/googleapis/gapic-generator-go/internal/pbinfo"
	"google.golang.org/genproto/googleapis/api/annotations"
)

func TestComment(t *testing.T) {
	var g generator

	for _, tst := range []struct {
		in, want string
	}{
		{
			in:   "",
			want: "",
		},
		{
			in:   "abc\ndef\n",
			want: "// abc\n// def\n",
		},
	} {
		g.pt.Reset()
		g.comment(tst.in)
		if got := g.pt.String(); got != tst.want {
			t.Errorf("comment(%q) = %q, want %q", tst.in, got, tst.want)
		}
	}
}

func TestMethodDoc(t *testing.T) {
	m := &descriptor.MethodDescriptorProto{
		Name: proto.String("MyMethod"),
	}

	var g generator
	g.comments = make(map[proto.Message]string)

	for _, tst := range []struct {
		in, want string
	}{
		{
			in:   "",
			want: "",
		},
		{
			in:   "Does stuff.\n It also does other stuffs.",
			want: "// MyMethod does stuff.\n// It also does other stuffs.\n",
		},
	} {
		g.comments[m] = tst.in
		g.pt.Reset()
		g.methodDoc(m)
		if got := g.pt.String(); got != tst.want {
			t.Errorf("comment(%q) = %q, want %q", tst.in, got, tst.want)
		}
	}
}

func TestReduceServName(t *testing.T) {
	for _, tst := range []struct {
		in, pkg, want string
	}{
		{"Foo", "", "Foo"},
		{"Foo", "foo", ""},

		{"FooV2", "", "Foo"},
		{"FooV2", "foo", ""},

		{"FooService", "", "Foo"},
		{"FooService", "foo", ""},

		{"FooServiceV2", "", "Foo"},
		{"FooServiceV2", "foo", ""},

		{"FooV2Bar", "", "FooV2Bar"},
	} {
		if got := pbinfo.ReduceServName(tst.in, tst.pkg); got != tst.want {
			t.Errorf("pbinfo.ReduceServName(%q, %q) = %q, want %q", tst.in, tst.pkg, got, tst.want)
		}
	}
}

func TestGRPCClientField(t *testing.T) {
	for _, tst := range []struct {
		in, pkg, want string
	}{
		{"Foo", "foo", "client"},
		{"FooV2", "foo", "client"},
		{"FooService", "foo", "client"},
		{"FooServiceV2", "foo", "client"},
		{"FooV2Bar", "", "fooV2BarClient"},
	} {
		if got := grpcClientField(pbinfo.ReduceServName(tst.in, tst.pkg)); got != tst.want {
			t.Errorf("grpcClientField(pbinfo.ReduceServName(%q, %q)) = %q, want %q", tst.in, tst.pkg, got, tst.want)
		}
	}
}

func TestGenMethod(t *testing.T) {
	inputType := &descriptor.DescriptorProto{
		Name: proto.String("InputType"),
	}
	outputType := &descriptor.DescriptorProto{
		Name: proto.String("OutputType"),
	}

	typep := func(t descriptor.FieldDescriptorProto_Type) *descriptor.FieldDescriptorProto_Type {
		return &t
	}
	labelp := func(l descriptor.FieldDescriptorProto_Label) *descriptor.FieldDescriptorProto_Label {
		return &l
	}

	pageInputType := &descriptor.DescriptorProto{
		Name: proto.String("PageInputType"),
		Field: []*descriptor.FieldDescriptorProto{
			{
				Name:  proto.String("page_size"),
				Type:  typep(descriptor.FieldDescriptorProto_TYPE_INT32),
				Label: labelp(descriptor.FieldDescriptorProto_LABEL_OPTIONAL),
			},
			{
				Name:  proto.String("page_token"),
				Type:  typep(descriptor.FieldDescriptorProto_TYPE_STRING),
				Label: labelp(descriptor.FieldDescriptorProto_LABEL_OPTIONAL),
			},
		},
	}
	pageOutputType := &descriptor.DescriptorProto{
		Name: proto.String("PageOutputType"),
		Field: []*descriptor.FieldDescriptorProto{
			{
				Name:  proto.String("next_page_token"),
				Type:  typep(descriptor.FieldDescriptorProto_TYPE_STRING),
				Label: labelp(descriptor.FieldDescriptorProto_LABEL_OPTIONAL),
			},
			{
				Name:  proto.String("items"),
				Type:  typep(descriptor.FieldDescriptorProto_TYPE_STRING),
				Label: labelp(descriptor.FieldDescriptorProto_LABEL_REPEATED),
			},
		},
	}

	file := &descriptor.FileDescriptorProto{
		Package: proto.String("my.pkg"),
		Options: &descriptor.FileOptions{
			GoPackage: proto.String("mypackage"),
		},
	}
	serv := &descriptor.ServiceDescriptorProto{}

	var g generator
	g.imports = map[pbinfo.ImportSpec]bool{}

	commonTypes(&g)
	for _, typ := range []*descriptor.DescriptorProto{
		inputType, outputType, pageInputType, pageOutputType,
	} {
		g.descInfo.Type[".my.pkg."+*typ.Name] = typ
		g.descInfo.ParentFile[typ] = file
	}
	g.descInfo.ParentFile[serv] = file

	meths := []*descriptor.MethodDescriptorProto{
		{
			Name:       proto.String("GetEmptyThing"),
			InputType:  proto.String(".my.pkg.InputType"),
			OutputType: proto.String(emptyType),
		},
		{
			Name:       proto.String("GetOneThing"),
			InputType:  proto.String(".my.pkg.InputType"),
			OutputType: proto.String(".my.pkg.OutputType"),
		},
		{
			Name:       proto.String("GetBigThing"),
			InputType:  proto.String(".my.pkg.InputType"),
			OutputType: proto.String(".google.longrunning.Operation"),
			Options:    &descriptor.MethodOptions{},
		},
		{
			Name:       proto.String("GetManyThings"),
			InputType:  proto.String(".my.pkg.PageInputType"),
			OutputType: proto.String(".my.pkg.PageOutputType"),
		},
		{
			Name:            proto.String("ServerThings"),
			InputType:       proto.String(".my.pkg.InputType"),
			OutputType:      proto.String(".my.pkg.OutputType"),
			ServerStreaming: proto.Bool(true),
		},
		{
			Name:            proto.String("ClientThings"),
			InputType:       proto.String(".my.pkg.InputType"),
			OutputType:      proto.String(".my.pkg.OutputType"),
			ClientStreaming: proto.Bool(true),
		},
		{
			Name:            proto.String("BidiThings"),
			InputType:       proto.String(".my.pkg.InputType"),
			OutputType:      proto.String(".my.pkg.OutputType"),
			ServerStreaming: proto.Bool(true),
			ClientStreaming: proto.Bool(true),
		},
	}

methods:
	for _, m := range meths {
		g.pt.Reset()

		// Just add this everywhere. Only LRO method will pick it up.
		if m.Options != nil {
			lroType := &annotations.LongrunningOperationTypes{
				Response: "OutputType",
			}
			proto.SetExtension(m.Options, annotations.E_LongrunningOperationTypes, lroType)
		}

		aux := auxTypes{
			iters: map[string]iterType{},
		}
		if err := g.genMethod("Foo", serv, m, &aux); err != nil {
			t.Error(err)
			continue
		}

		for _, m := range aux.lros {
			if err := g.lroType("MyService", serv, m); err != nil {
				t.Error(err)
				continue methods
			}
		}

		for _, iter := range aux.iters {
			g.pagingIter(iter)
		}

		diff(t, m.GetName(), g.pt.String(), filepath.Join("testdata", "method_"+m.GetName()+".want"))
	}
}
