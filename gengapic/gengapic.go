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
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gapic-generator-go/internal/errors"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/protoc-gen-go/descriptor"
	plugin "github.com/golang/protobuf/protoc-gen-go/plugin"
)

const (
	// protoc puts a dot in front of name, signaling that the name is fully qualified.
	emptyType = ".google.protobuf.Empty"
	lroType   = ".google.longrunning.Operation"
)

var tabsCache = strings.Repeat("\t", 20)
var spacesCache = strings.Repeat(" ", 100)

func Gen(genReq *plugin.CodeGeneratorRequest) (*plugin.CodeGeneratorResponse, error) {
	var pkgPath, pkgName string
	if genReq.Parameter == nil {
		return nil, errors.E(nil, "need parameter in format: client/import/path;packageName")
	}
	if p := strings.IndexByte(*genReq.Parameter, ';'); p < 0 {
		return nil, errors.E(nil, "need parameter in format: client/import/path;packageName")
	} else {
		pkgPath = (*genReq.Parameter)[:p]
		pkgName = (*genReq.Parameter)[p+1:]
	}
	outDir := filepath.FromSlash(pkgPath)

	var g generator
	g.init(genReq.ProtoFile)

	var genServs []*descriptor.ServiceDescriptorProto
	for _, f := range genReq.ProtoFile {
		if strContains(genReq.FileToGenerate, *f.Name) {
			genServs = append(genServs, f.Service...)
		}
	}

	for _, s := range genServs {
		// TODO(pongad): gapic-generator does not remove the package name here,
		// so even though the client for LoggingServiceV2 is just "Client"
		// the file name is "logging_client.go".
		// Keep the current behavior for now, but we could revisit this later.
		outFile := reduceServName(s.GetName(), "")
		outFile = camelToSnake(outFile)
		outFile = filepath.Join(outDir, outFile)

		g.reset()
		if err := g.gen(s, pkgName); err != nil {
			return nil, errors.E(err, "service: %s", s.GetName())
		}
		g.commit(outFile+"_client.go", pkgName)

		g.reset()
		if err := g.genExampleFile(s, pkgName); err != nil {
			return nil, errors.E(err, "example: %s", s.GetName())
		}
		g.imports[importSpec{path: pkgPath}] = true
		g.commit(outFile+"_client_example_test.go", pkgName+"_test")
	}

	g.reset()
	scopes, err := collectScopes(genServs)
	if err != nil {
		return nil, err
	}
	g.genDocFile(pkgPath, pkgName, time.Now().Year(), scopes)
	g.resp.File = append(g.resp.File, &plugin.CodeGeneratorResponse_File{
		Name:    proto.String(filepath.Join(outDir, "doc.go")),
		Content: proto.String(g.sb.String()),
	})

	return &g.resp, nil
}

func strContains(a []string, s string) bool {
	for _, as := range a {
		if as == s {
			return true
		}
	}
	return false
}

type generator struct {
	sb strings.Builder

	// current indentation level
	in int

	resp plugin.CodeGeneratorResponse

	// Maps services and messages to the file containing them,
	// so we can figure out the import.
	parentFile map[proto.Message]*descriptor.FileDescriptorProto

	// Maps type names to their messages
	types map[string]*descriptor.DescriptorProto

	// Maps proto elements to their comments
	comments map[proto.Message]string

	imports map[importSpec]bool
}

func (g *generator) init(files []*descriptor.FileDescriptorProto) {
	g.parentFile = map[proto.Message]*descriptor.FileDescriptorProto{}
	g.types = map[string]*descriptor.DescriptorProto{}
	g.comments = map[proto.Message]string{}
	g.imports = map[importSpec]bool{}

	for _, f := range files {
		// parentFile
		for _, m := range f.MessageType {
			g.parentFile[m] = f
		}
		for _, s := range f.Service {
			g.parentFile[s] = f
		}

		// types
		for _, m := range f.MessageType {
			// In descriptors, putting the dot in front means the name is fully-qualified.
			fullyQualifiedName := fmt.Sprintf(".%s.%s", f.GetPackage(), m.GetName())
			g.types[fullyQualifiedName] = m
		}

		// comment
		for _, loc := range f.GetSourceCodeInfo().GetLocation() {
			// p is an array with format [f1, i1, f2, i2, ...]
			// - f1 refers to the protobuf field tag
			// - if field refer to by f1 is a slice, i1 refers to an element in that slice
			// - f2 and i2 works recursively.
			// So, [6, x] refers to the xth service defined in the file,
			// since the field tag of Service is 6.
			// [6, x, 2, y] refers to the yth method in that service,
			// since the field tag of Method is 2.
			p := loc.Path
			switch {
			case len(p) == 2 && p[0] == 6:
				g.comments[f.Service[p[1]]] = *loc.LeadingComments
			case len(p) == 4 && p[0] == 6 && p[2] == 2:
				g.comments[f.Service[p[1]].Method[p[3]]] = *loc.LeadingComments
			}
		}
	}
}

// importSpec reports the importSpec for package containing protobuf element e.
func (g *generator) importSpec(e proto.Message) (importSpec, error) {
	fdesc := g.parentFile[e]

	pkg := fdesc.GetOptions().GetGoPackage()
	if pkg == "" {
		return importSpec{}, errors.E(nil, "can't determine import path, file %q missing `option go_package`", fdesc.GetName())
	}

	if p := strings.IndexByte(pkg, ';'); p >= 0 {
		return importSpec{path: pkg[:p], name: pkg[p+1:] + "pb"}, nil
	}

	for {
		p := strings.LastIndexByte(pkg, '/')
		if p < 0 {
			return importSpec{path: pkg, name: pkg + "pb"}, nil
		}
		elem := pkg[p+1:]
		if len(elem) >= 2 && elem[0] == 'v' && elem[1] >= '0' && elem[1] <= '9' {
			// It's a version number; skip so we get a more meaningful name
			pkg = pkg[:p]
			continue
		}
		return importSpec{path: pkg, name: elem + "pb"}, nil
	}
}

// printf formatted-prints to sb, using the print syntax from fmt package.
//
// It automatically keeps track of indentation caused by curly-braces.
// To make nested blocks easier to write elsewhere in the code,
// leading and trailing whitespaces in s are ignored.
// These spaces are for humans reading the code, not machines.
//
// Currently it's not terribly difficult to confuse the auto-indenter.
// To fix-up, manipulate g.in or write to g.sb directly.
func (g *generator) printf(s string, a ...interface{}) {
	s = strings.TrimSpace(s)
	if s == "" {
		g.sb.WriteByte('\n')
		return
	}

	for i := 0; i < len(s) && s[i] == '}'; i++ {
		g.in--
	}

	in := g.in
	for in > len(tabsCache) {
		g.sb.WriteString(tabsCache)
		in -= len(tabsCache)
	}
	g.sb.WriteString(tabsCache[:in])

	fmt.Fprintf(&g.sb, s, a...)
	g.sb.WriteByte('\n')

	for i := len(s) - 1; i >= 0 && s[i] == '{'; i-- {
		g.in++
	}
}

func (g *generator) commit(fileName, pkgName string) {
	var header strings.Builder
	fmt.Fprintf(&header, apacheLicense, time.Now().Year())
	fmt.Fprintf(&header, "package %s\n\n", pkgName)

	var imps []importSpec
	for imp := range g.imports {
		imps = append(imps, imp)
	}
	impDiv := sortImports(imps)

	writeImp := func(is importSpec) {
		s := "\t%[2]q\n"
		if is.name != "" {
			s = "\t%s %q\n"
		}
		fmt.Fprintf(&header, s, is.name, is.path)
	}

	header.WriteString("import (\n")
	for _, imp := range imps[:impDiv] {
		writeImp(imp)
	}
	if impDiv != 0 && impDiv != len(imps) {
		header.WriteByte('\n')
	}
	for _, imp := range imps[impDiv:] {
		writeImp(imp)
	}
	header.WriteString(")\n\n")

	g.resp.File = append(g.resp.File, &plugin.CodeGeneratorResponse_File{
		Name:    &fileName,
		Content: proto.String(header.String()),
	})

	// Trim trailing newlines so we have only one.
	// NOTE(pongad): This might be an overkill since we have gofmt,
	// but the rest of the file already conforms to gofmt, so we might as well?
	body := g.sb.String()
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	for i := len(body) - 1; i >= 0; i-- {
		if body[i] != '\n' {
			body = body[:i+2]
			break
		}
	}

	g.resp.File = append(g.resp.File, &plugin.CodeGeneratorResponse_File{
		Content: proto.String(body),
	})
}

func (g *generator) reset() {
	g.sb.Reset()
	g.in = 0
	for k := range g.imports {
		delete(g.imports, k)
	}
}

func (g *generator) gen(serv *descriptor.ServiceDescriptorProto, pkgName string) error {
	servName := reduceServName(*serv.Name, pkgName)
	if err := g.clientOptions(serv, servName); err != nil {
		return err
	}
	if err := g.clientInit(serv, servName); err != nil {
		return err
	}

	// Methods to generate LRO and iterator types for. Populated as we go.
	var (
		lroMethods []*descriptor.MethodDescriptorProto
		iterTypes  = map[string]iterType{}
	)

	// TODO(pongad): Move content of loop into a method, so we can unit-test
	// Do this after https://gapi-review.git.corp.google.com/c/gapic-generator-go/+/6870
	// so we don't need to resolve conflicts.
	var err error
	for _, m := range serv.Method {
		if err != nil {
			return err
		}

		g.methodDoc(m)

		if *m.OutputType == lroType {
			lroMethods = append(lroMethods, m)
			err = g.lroCall(servName, m)
			continue
		}

		if *m.OutputType == emptyType {
			err = g.emptyUnaryCall(servName, m)
			continue
		}

		if pf, err := g.pagingField(m); err != nil {
			return err
		} else if pf != nil {
			iter, err := g.iterTypeOf(pf)
			if err != nil {
				return err
			}
			iterTypes[iter.iterTypeName] = iter
			err = g.pagingCall(servName, m, pf, iter)
			continue
		}

		switch {
		case m.GetClientStreaming() && m.GetServerStreaming():
			err = g.bidiCall(servName, serv, m)
		default:
			err = g.unaryCall(servName, m)
		}
	}
	if err != nil {
		return err
	}

	sort.Slice(lroMethods, func(i, j int) bool {
		return *lroMethods[i].Name < *lroMethods[j].Name
	})
	for _, m := range lroMethods {
		g.lroType(servName, m)
	}

	var iters []iterType
	for _, iter := range iterTypes {
		iters = append(iters, iter)
	}
	sort.Slice(iters, func(i, j int) bool {
		return iters[i].iterTypeName < iters[j].iterTypeName
	})
	for _, iter := range iters {
		g.pagingIter(iter)
	}

	return nil
}

func (g *generator) unaryCall(servName string, m *descriptor.MethodDescriptorProto) error {
	inType := g.types[*m.InputType]
	outType := g.types[*m.OutputType]

	inSpec, err := g.importSpec(inType)
	if err != nil {
		return err
	}
	outSpec, err := g.importSpec(outType)
	if err != nil {
		return err
	}

	p := g.printf

	p("func (c *%sClient) %s(ctx context.Context, req *%s.%s, opts ...gax.CallOption) (*%s.%s, error) {",
		servName, *m.Name, inSpec.name, *inType.Name, outSpec.name, *outType.Name)

	g.insertMetadata()
	g.appendCallOpts(m)
	p("var resp *%s.%s", outSpec.name, *outType.Name)
	p("err := gax.Invoke(ctx, func(ctx context.Context, settings gax.CallSettings) error {")
	p("  var err error")
	p("  resp, err = %s", grpcClientCall(servName, *m.Name))
	p("  return err")
	p("}, opts...)")
	p("if err != nil {")
	p("  return nil, err")
	p("}")
	p("return resp, nil")

	p("}")
	p("")

	g.imports[inSpec] = true
	g.imports[outSpec] = true

	return nil
}

func (g *generator) emptyUnaryCall(servName string, m *descriptor.MethodDescriptorProto) error {
	inType := g.types[*m.InputType]

	inSpec, err := g.importSpec(inType)
	if err != nil {
		return err
	}

	p := g.printf

	p("func (c *%sClient) %s(ctx context.Context, req *%s.%s, opts ...gax.CallOption) error {",
		servName, m.GetName(), inSpec.name, inType.GetName())

	g.insertMetadata()
	g.appendCallOpts(m)
	p("err := gax.Invoke(ctx, func(ctx context.Context, settings gax.CallSettings) error {")
	p("  var err error")
	p("  _, err = %s", grpcClientCall(servName, m.GetName()))
	p("  return err")
	p("}, opts...)")
	p("return err")

	p("}")
	p("")

	g.imports[inSpec] = true
	return nil
}

func (g *generator) insertMetadata() {
	g.printf("ctx = insertMetadata(ctx, c.xGoogMetadata)")
}

func (g *generator) appendCallOpts(m *descriptor.MethodDescriptorProto) {
	g.printf("opts = append(%[1]s[0:len(%[1]s):len(%[1]s)], opts...)", "c.CallOptions."+*m.Name)
}

func (g *generator) methodDoc(m *descriptor.MethodDescriptorProto) {
	com := g.comments[m]
	com = strings.TrimSpace(com)

	// If there's no comment, adding method name is just confusing.
	if com == "" {
		return
	}

	g.comment(*m.Name + " " + lowerFirst(com))
}

func (g *generator) comment(s string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return
	}

	s = MDPlain(s)

	lines := strings.Split(s, "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			g.printf("//")
		} else {
			g.printf("// %s", l)
		}
	}
}

func spaces(n int) string {
	if n > len(spacesCache) {
		return strings.Repeat(" ", n)
	}
	return spacesCache[:n]
}

// reduceServName removes redundant components from the service name.
// For example, FooServiceV2 -> Foo.
// The returned name is used as part of longer names, like FooClient.
// If the package name and the service name is the same,
// reduceServName returns empty string, so we get foo.Client instead of foo.FooClient.
func reduceServName(svc, pkg string) string {
	// remove trailing version
	if p := strings.LastIndexByte(svc, 'V'); p >= 0 {
		isVer := true
		for _, r := range svc[p+1:] {
			if !unicode.IsDigit(r) {
				isVer = false
				break
			}
		}
		if isVer {
			svc = svc[:p]
		}
	}

	svc = strings.TrimSuffix(svc, "Service")
	if strings.EqualFold(svc, pkg) {
		svc = ""
	}
	return svc
}

// grpcClientField reports the field name to store gRPC client.
func grpcClientField(reducedServName string) string {
	// Not the same as reduceServName(*serv.Name, pkg)+"Client".
	// If the service name is reduced to empty string, we should
	// lower-case "client" so that the field is not exported.
	return lowerFirst(reducedServName + "Client")
}

func grpcClientCall(reducedServName, methName string) string {
	return fmt.Sprintf("c.%s.%s(ctx, req, settings.GRPC...)", grpcClientField(reducedServName), methName)
}

func lowerFirst(s string) string {
	if s == "" {
		return ""
	}
	r, w := utf8.DecodeRuneInString(s)
	return string(unicode.ToLower(r)) + s[w:]
}

func upperFirst(s string) string {
	if s == "" {
		return ""
	}
	r, w := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[w:]
}

func camelToSnake(s string) string {
	var sb strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) && i != 0 {
			sb.WriteByte('_')
		}
		sb.WriteRune(unicode.ToLower(r))
	}
	return sb.String()
}

func snakeToCamel(s string) string {
	var sb strings.Builder
	up := true
	for _, r := range s {
		if r == '_' {
			up = true
		} else if up {
			sb.WriteRune(unicode.ToUpper(r))
			up = false
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
