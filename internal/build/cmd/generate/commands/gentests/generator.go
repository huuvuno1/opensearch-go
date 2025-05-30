// SPDX-License-Identifier: Apache-2.0
//
// The OpenSearch Contributors require contributions made to
// this file be licensed under the Apache-2.0 license or a
// compatible open source license.
//
// Modifications Copyright OpenSearch Contributors. See
// GitHub history for details.

// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package gentests

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/tools/imports"
)

var (
	goruncache map[string]string
)

func init() {
	goruncache = make(map[string]string)
}

// Generator represents the "gentests" generator.
type Generator struct {
	b bytes.Buffer

	TestSuite TestSuite
}

// Output returns the generator output.
func (g *Generator) Output() (io.Reader, error) {
	name := g.TestSuite.Name()

	g.genFileHeader()
	g.w("func Test" + name + "(t *testing.T) {\n")
	g.genInitializeClient()
	g.genHelpers()
	g.genCommonSetup()

	if len(g.TestSuite.Setup) > 0 {
		g.w("// ----- Test Suite Setup --------------------------------------------------------\n")
		g.w("testSuiteSetup := func() {\n")
		g.genSetupTeardown(g.TestSuite.Setup)
		g.w("}\n")
		g.w("_ = testSuiteSetup\n")
		g.w("// --------------------------------------------------------------------------------\n")
		g.w("\n")
	}
	if len(g.TestSuite.Teardown) > 0 {
		g.w("\t// Teardown\n")
		g.w("\tdefer func(t *testing.T) {\n")
		g.genSetupTeardown(g.TestSuite.Teardown)
		g.w("\t}(t)\n")
	}
	for i, t := range g.TestSuite.Tests {
		g.w("\n")
		g.genLocationYAML(t)
		g.w("\t" + `t.Run("` + strings.ReplaceAll(t.Name, " ", "_") + `", ` + "func(t *testing.T) {\n")
		if !g.genSkip(t) {
			g.w("\tdefer recoverPanic(t)\n")
			g.w("\tcommonSetup()\n")
			if len(g.TestSuite.Setup) > 0 {
				g.w("\ttestSuiteSetup()\n")
			}
			g.w("\n")
			if len(t.Setup) > 0 {
				g.w("\t// Test setup\n")
				g.genSetupTeardown(t.Setup)
			}
			if len(t.Teardown) > 0 {
				g.w("\t// Test teardown\n")
				g.w("\tdefer func(t) {\n")
				g.genSetupTeardown(t.Teardown)
				g.w("\t}(t *testing.T)\n")
			}
			if len(t.Setup) > 0 || len(t.Teardown) > 0 {
				g.w("\n")
			}
			g.genSteps(t)
		}
		g.w("\t})\n")
		if i < len(g.TestSuite.Tests)-1 {
			g.w("\n")
		}
	}
	g.w("}\n")
	return bytes.NewReader(g.b.Bytes()), nil
}

// OutputFormatted returns a formatted generator output.
func (g *Generator) OutputFormatted() (io.Reader, error) {
	out, err := g.Output()
	if err != nil {
		return bytes.NewReader(g.b.Bytes()), err
	}

	var b bytes.Buffer
	if _, err := io.Copy(&b, out); err != nil {
		return bytes.NewReader(g.b.Bytes()), err
	}

	fout, err := imports.Process(
		"",
		g.b.Bytes(),
		&imports.Options{
			AllErrors:  true,
			Comments:   true,
			FormatOnly: false,
			TabIndent:  true,
			TabWidth:   1,
		})
	if err != nil {
		return bytes.NewReader(b.Bytes()), err
	}

	g.b.Reset()
	g.b.Write(fout)

	return bytes.NewReader(fout), nil
}

func (g *Generator) w(s string) {
	g.b.WriteString(s)
}

func (g *Generator) gorun(code string) (string, error) {
	if goruncache[code] != "" {
		return goruncache[code], nil
	}

	dir, err := ioutil.TempDir("tmp", "gorun")
	if err != nil {
		return "", fmt.Errorf("gorun: %s", err)
	}
	f, err := os.Create(filepath.Join(dir, "type_for_struct_field.go"))
	if err != nil {
		return "", fmt.Errorf("gorun: %s", err)
	}
	defer func() {
		f.Close()
		os.RemoveAll(dir)
	}()

	// fmt.Println(code)
	if err := ioutil.WriteFile(f.Name(), []byte(code), 0644); err != nil {
		return "", fmt.Errorf("gorun: %s", err)
	}

	cmd := exec.Command("go", "run", f.Name())
	out, err := cmd.Output()
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("gorun: %s", e.Stderr)
		}
		return "", fmt.Errorf("gorun: %s", err)
	}

	goruncache[code] = string(out)

	return string(out), nil
}

func (g *Generator) genFileHeader() {
	g.w("// Code generated")
	if EsVersion != "" || GitCommit != "" || GitTag != "" {
		g.w(" from YAML test suite version")
		if GitCommit != "" {
			g.w(fmt.Sprintf(" %s", GitCommit))
			if GitTag != "" {
				g.w(fmt.Sprintf("|%s", GitTag))
			}
		}
	}
	g.w(" -- DO NOT EDIT\n")
	g.w("\n")
	g.w("package opensearchapi_test\n")
	g.w(`
import (
	encjson "encoding/json"
	encyaml "gopkg.in/yaml.v2"
	"fmt"
	"context"
	"crypto/tls"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/huuvuno1/opensearch-go/v2"
	"github.com/huuvuno1/opensearch-go/v2/opensearchapi"
	"github.com/huuvuno1/opensearch-go/v2/opensearchtransport"
)

var (
	// Prevent compilation errors for unused packages
	_ = fmt.Printf
	_ = encjson.NewDecoder
	_ = encyaml.NewDecoder
	_ = tls.Certificate{}
	_ = url.QueryEscape
)` + "\n")
}

func (g *Generator) genInitializeClient() {
	g.w(`
	cfg := opensearch.Config{}
	`)

	g.w(`
			if os.Getenv("DEBUG") != "" {
				cfg.Logger = &opensearchtransport.ColorLogger{
					Output: os.Stdout,
					// EnableRequestBody:  true,
					EnableResponseBody: true,
				}
			}` + "\n")

	g.w(`
	client, eserr := opensearch.NewClient(cfg)
	if eserr != nil {
		t.Fatalf("Error creating the client: %s\n", eserr)
	}

`)
}

func (g *Generator) genHelpers() {
	g.w(`recoverPanic := func(t *testing.T) {
	reLocation := regexp.MustCompile("(.*_test.go:\\d+).*")
	if rec := recover(); rec != nil {
		var loc string
		s := strings.Split(string(debug.Stack()), "\n")
		for i := len(s) - 1; i >= 0; i-- {
			if reLocation.MatchString(s[i]) {
				loc = strings.TrimSpace(s[i])
				break
			}
		}
		t.Fatalf("Panic: %s in %s", rec, reLocation.ReplaceAllString(loc, "$1"))
	}
}
_ = recoverPanic
` + "\n")

	g.w(`
	handleResponseError := func(t *testing.T, res *opensearchapi.Response) {
		if res.IsError() {
			reLocation := regexp.MustCompile("(.*_test.go:\\d+).*")
			var loc string
			s := strings.Split(string(debug.Stack()), "\n")
			for i := len(s) - 1; i >= 0; i-- {
				if reLocation.MatchString(s[i]) {
					loc = strings.TrimSpace(s[i])
					break
				}
			}
			t.Logf("Response error: %s in %s", res, reLocation.ReplaceAllString(loc, "$1"))
		}
	}
	_ = handleResponseError
`)
	g.w("\n\n")
}

// Reference: https://github.com/opensearch-project/OpenSearch/blob/main/test/framework/src/main/java/org/opensearch/test/rest/OpenSearchRestTestCase.java
func (g *Generator) genCommonSetup() {
	g.w(`
	// ----- Common Setup -------------------------------------------------------------
	commonSetup := func() {
		var res *opensearchapi.Response

		{
			res, _ = client.Cluster.Health(client.Cluster.Health.WithWaitForNoInitializingShards(true))
			if res != nil && res.Body != nil {
				defer res.Body.Close()
			}
		}

		{
			res, _ = client.Indices.Delete(
				[]string{"*"},
				client.Indices.Delete.WithExpandWildcards("all"))
			if res != nil && res.Body != nil {
				defer res.Body.Close()
			}
		}

		{
			var r map[string]interface{}
			res, _ = client.Indices.GetTemplate()
			if res != nil && res.Body != nil {
				defer res.Body.Close()
				json.NewDecoder(res.Body).Decode(&r)
				for templateName, _ := range r {
					if strings.HasPrefix(templateName, ".") {
						continue
					}
					if templateName == "security_audit_log" {
						continue
					}
					if templateName == "logstash-index-template" {
						continue
					}
					client.Indices.DeleteTemplate(templateName)
				}
			}
		}

		{
			res, _ = client.Indices.DeleteIndexTemplate("*")
			if res != nil && res.Body != nil { defer res.Body.Close() }
		}

		{
			res, _ = client.Indices.DeleteAlias([]string{"_all"}, []string{"_all"})
			if res != nil && res.Body != nil { defer res.Body.Close() }
		}

		{
			var r map[string]interface{}
			res, _ = client.Snapshot.GetRepository()
			if res != nil && res.Body != nil {
				defer res.Body.Close()
				json.NewDecoder(res.Body).Decode(&r)
				for repositoryID, _ := range r {
					var r map[string]interface{}
					res, _ = client.Snapshot.Get(repositoryID, []string{"_all"})
					json.NewDecoder(res.Body).Decode(&r)
					if r["responses"] != nil {
						for _, vv := range r["responses"].([]interface{}) {
							for _, v := range vv.(map[string]interface{})["snapshots"].([]interface{}) {
								snapshotID, ok := v.(map[string]interface{})["snapshot"]
								if !ok {
									continue
								}
								client.Snapshot.Delete(repositoryID, fmt.Sprintf("%s", snapshotID))
							}
						}
					}
					client.Snapshot.DeleteRepository([]string{fmt.Sprintf("%s", repositoryID)})
				}
			}
		}

		{
			res, _ = client.Cluster.Health(client.Cluster.Health.WithWaitForStatus("yellow"))
			if res != nil && res.Body != nil {
				defer res.Body.Close()
			}
		}
	}
	_ = commonSetup

	`)
}

func (g *Generator) genLocationYAML(t Test) {
	f, err := os.Open(t.Filepath)
	if err != nil {
		g.w(fmt.Sprintf("// Error opening file: %s\n", err))
	}

	scanner := bufio.NewScanner(f)
	var i int
	for scanner.Scan() {
		i++
		tname := scanner.Text()
		tname = strings.TrimRight(tname, `:`)
		tname = strings.NewReplacer(`\"`, `"`).Replace(tname)
		tname = strings.TrimPrefix(tname, `"`)
		tname = strings.TrimSuffix(tname, `"`)

		if tname == t.OrigName {
			// TODO: Github URL (with proper branch/commit/etc)
			g.w("\t// Source: " + t.Filepath + fmt.Sprintf(":%d", i) + "\n\t//\n")
		}
	}
	if err := scanner.Err(); err != nil {
		g.w(fmt.Sprintf("// Error reading file: %s\n", err))
	}
}

func (g *Generator) genSkip(t Test) (skipped bool) {
	// Check the custom skip list
	if skips, ok := skipTests[t.BaseFilename()]; ok {
		if len(skips) < 1 {
			g.w("\t// Skipping all tests in '" + t.BaseFilename() + "'\n")
			g.w("\tt.SkipNow()\n\n")
			return true
		}

		for _, skip := range skips {
			if skip == t.OrigName {
				g.w("\tt.SkipNow()\n\n")
				return true
			}
		}
	}

	// Check the skip property coming from YAML
	if t.Skip {
		if t.SkipInfo != "" {
			g.w("\tt.Skip(" + strconv.Quote(t.SkipInfo) + ")\n\n")
			return true
		} else {
			g.w("\tt.SkipNow()\n\n")
			return true
		}
	}

	return false
}

func (g *Generator) genSetupTeardown(actions []Action) {
	g.genVarSection(Test{})

	for _, a := range actions {
		g.genAction(a, false)
		g.w("\n")
	}
}

func (g *Generator) genSteps(t Test) {
	var skipBody bool
	if !t.Steps.ContainsAssertion() && !t.Steps.ContainsCatch() && !t.Steps.ContainsStash() {
		skipBody = true
	}
	g.genVarSection(t, skipBody)

	for _, step := range t.Steps {
		switch step.(type) {
		case Action:
			// Generate debug info
			var dbg strings.Builder
			dbg.WriteString("\t\t// => " + step.(Action).Method() + "(")
			var j int
			for k, v := range step.(Action).Params() {
				j++
				dbg.WriteString(k + ": " + strings.Replace(fmt.Sprintf("%v", v), "\n", "|", -1))
				if j < len(step.(Action).Params()) {
					dbg.WriteString(", ")
				}
			}
			dbg.WriteString(") ")
			pad := 101 - dbg.Len()
			if pad < 0 {
				pad = 0
			}
			g.w(dbg.String() + strings.Repeat("-", pad) + "\n\t\t//\n")

			// Generate the action
			g.genAction(step.(Action), skipBody)
			g.w("\t\t// " + strings.Repeat("-", 96) + "\n\n")
		case Assertion:
			// Generate debug info
			g.w("\t\t// ~> ")
			g.w(fmt.Sprintf("%q: ", step.(Assertion).operation))
			g.w(strings.Replace(fmt.Sprintf("%s", step.(Assertion).payload), "\n", "|", -1))
			g.w("\n")
			// Generate the assertion
			g.genAssertion(step.(Assertion))
			g.w("\n")
		case Stash:
			// Generate setting the stash
			g.genStashSet(step.(Stash))
			g.w("\n")
		default:
			panic(fmt.Sprintf("Unknown step %T", step))
		}
	}
}

func (g *Generator) genVarSection(t Test, skipBody ...bool) {
	g.w("\t\tvar (\n")
	g.w("\t\t\treq opensearchapi.Request\n")
	g.w("\t\t\tres *opensearchapi.Response\n")
	g.w("\t\t\terr error\n\n")

	g.w("\t\t\tstash = make(map[string]interface{}, 0)\n\n")

	if (len(skipBody) < 1 || (len(skipBody) > 0 && skipBody[0] == false)) &&
		(t.Steps.ContainsAssertion() || t.Steps.ContainsCatch() || true) {
		g.w("\t\t\tbody []byte\n")
		g.w("\t\t\tmapi map[string]interface{}\n")
		g.w("\t\t\tslic []interface{}\n")
	}

	if t.Steps.ContainsAssertion("is_false", "is_true") {
		g.w("\n\t\t\tvalue reflect.Value\n")
	}

	g.w("\n")
	g.w("\t\t\tassertion bool\n")

	g.w("\t\t\tactual   interface{}\n")
	g.w("\t\t\texpected interface{}\n")
	g.w("\n")

	if t.Steps.ContainsAssertion("match", "match-regexp") {
		g.w("\n\t\t\tre *regexp.Regexp\n")
		g.w("\t\t\tmatch bool\n")
	}

	g.w("\t\t)\n\n")

	if (len(skipBody) < 1 || (len(skipBody) > 0 && skipBody[0] == false)) &&
		(t.Steps.ContainsAssertion() || t.Steps.ContainsCatch() || true) {
		g.w("\t\t_ = mapi\n")
		g.w("\t\t_ = slic\n")
		g.w("\n")
		g.w(`handleResponseBody := func(t *testing.T, res *opensearchapi.Response) {
			// Reset deserialized structures
			mapi = make(map[string]interface{})
			slic = make([]interface{}, 0)

			var err error
			body, err = ioutil.ReadAll(res.Body)
			if err != nil {
				t.Fatalf("Error reading body: %s", err)
			}
			res.Body.Close()
			res.Body = ioutil.NopCloser(bytes.NewBuffer(body))

			if len(body) < 1 {
				// FIXME: Hack to prevent EOF errors
				return
			}

			if len(res.Header) > 0 {
				if strings.Contains(res.Header["Content-Type"][0], "text/plain") {
					return
				}

				if strings.Contains(res.Header["Content-Type"][0], "yaml") {
					if strings.HasPrefix(string(body), "---\n-") {
						if err := encyaml.NewDecoder(res.Body).Decode(&slic); err != nil {
							t.Fatalf("Error parsing the response body: %s", err)
						}
					} else {
						if err := encyaml.NewDecoder(res.Body).Decode(&mapi); err != nil {
							t.Fatalf("Error parsing the response body: %s", err)
						}
					}
					return
				}
			}

			d := encjson.NewDecoder(res.Body)
			d.UseNumber()

			if strings.HasPrefix(string(body), "[") {
				if err := d.Decode(&slic); err != nil {
					t.Fatalf("Error parsing the response body: %s", err)
				}
			} else {
				if err := d.Decode(&mapi); err != nil {
					t.Fatalf("Error parsing the response body: %s", err)
				}
			}
		}` + "\n")
	}

	g.w("\n")

	g.w("\t\t_ = stash\n")

	if t.Steps.ContainsAssertion("is_false", "is_true") {
		g.w("\t\t_ = value\n")
	}

	g.w("\t\t_ = assertion\n")

	g.w("\t\t_ = actual\n")
	g.w("\t\t_ = expected\n")

	if t.Steps.ContainsAssertion("match", "match-regexp") {
		g.w("\n")
		g.w("\t\t_ = re\n")
		g.w("\t\t_ = match\n")
	}

	g.w("\n")
}

func (g *Generator) genAction(a Action, skipBody ...bool) {
	// Initialize the request
	g.w("\t\treq = opensearchapi." + a.Request() + "{\n")

	// Pass the parameters
	for k, v := range a.Params() {
		// fmt.Printf("%s.%s: <%T> %v\n", a.Request(), k, v, v)

		if strings.HasPrefix(fmt.Sprintf("%s", v), "$") {
			v = `stash[` + strconv.Quote(strings.ReplaceAll(strings.ReplaceAll(fmt.Sprintf("%s", v), "{", ""), "}", "")) + `]`
		}

		switch v.(type) {
		case bool:
			g.w("\t\t\t" + k + ": ")

			typ, ok := apiRegistry[a.Request()][k]
			if !ok {
				panic(fmt.Sprintf("%s.%s: field not found", a.Request(), k))
			}

			switch typ {
			case "bool":
				g.w(strconv.FormatBool(v.(bool)))
			case "*bool":
				g.w(`opensearchapi.BoolPtr(` + strconv.FormatBool(v.(bool)) + `)`)
			case "string":
				g.w(`"` + strconv.FormatBool(v.(bool)) + `"`)
			case "[]string":
				// TODO: Listify
				g.w(`[]string{"` + strconv.FormatBool(v.(bool)) + `"}`)
			default:
				g.w(strconv.FormatBool(v.(bool)))
			}
			g.w(",\n")

		case string:
			if k == "Body" {
				g.w("\t\t\t" + k + ": ")
				body := v.(string)
				if !strings.HasSuffix(body, "\n") {
					body = body + "\n"
				}
				g.w("strings.NewReader(`" + body + "`)")
			} else {
				g.w("\t\t\t" + k + ": ")
				// TODO: Handle comma separated strings as lists

				// fmt.Printf("%s: %#v\n", a.Request(), apiRegistry[a.Request()])
				// fmt.Printf("%s: %#v\n", k, apiRegistry[a.Request()][k])
				typ, ok := apiRegistry[a.Request()][k]
				if !ok {
					panic(fmt.Sprintf("%s.%s: field not found", a.Request(), k))
				}

				var value string
				if strings.HasPrefix(v.(string), "stash[") {
					switch typ {
					case "bool":
						value = `fmt.Sprintf("%v", ` + v.(string) + `)`
					case "string":
						value = fmt.Sprintf("%s.(string)", v)
					case "[]string":
						// TODO: Comma-separated list => Quoted list
						value = fmt.Sprintf(`[]string{%s.(string)}`, v)
					case "int":
						value = `func() int {
				switch ` + v.(string) + `.(type) {
				case int:
					return ` + v.(string) + `.(int)
				case float64:
					return int(` + v.(string) + `.(float64))
				}
				case json.Number:
					v, _ := ` + v.(string) + `.(encjson.Number).Int64()
					vv := int(v)
					return vv
				panic(fmt.Sprintf(` + "`" + `Unexpected type %T for ` + v.(string) + "`" + `, ` + v.(string) + `))
			}()`
					case "*int":
						value = `func() *int {
				switch ` + v.(string) + `.(type) {
				case int:
					v := ` + v.(string) + `.(int)
					return &v
				case float64:
					v := int(` + v.(string) + `.(float64))
					return &v
				case json.Number:
					v, _ := ` + v.(string) + `.(encjson.Number).Int64()
					vv := int(v)
					return &vv
				}
				panic(fmt.Sprintf(` + "`" + `Unexpected type %T for ` + v.(string) + "`" + `, ` + v.(string) + `))
			}()`
					case "time.Duration":
						value = `fmt.Sprintf("%d", ` + v.(string) + `)`
					default:
						panic(fmt.Sprintf("Unexpected type %q for value %v", typ, v))
					}
				} else {
					switch typ {
					case "[]string":
						value = `[]string{` + fmt.Sprintf("%q", v) + `}`
					case "time.Duration":
						// re := regexp.MustCompile("^(\\d+).*")
						// value = re.ReplaceAllString(fmt.Sprintf("%s", v), "$1")
						inputValue := v.(string)
						if strings.HasSuffix(inputValue, "d") {
							inputValue = inputValue[:len(inputValue)-1]
							numericValue, err := strconv.Atoi(inputValue)
							if err != nil {
								panic(fmt.Sprintf("Cannot convert duration [%s]: %s", inputValue, err))
							}
							// Convert to hours
							inputValue = fmt.Sprintf("%dh", numericValue*24)
						}

						dur, err := time.ParseDuration(inputValue)
						if err != nil {
							panic(fmt.Sprintf("Cannot parse duration [%s]: %s", v, err))
						}
						value = fmt.Sprintf("%d", dur.Nanoseconds())
					default:
						if strings.HasSuffix(k, "ID") {
							value = fmt.Sprintf("url.QueryEscape(%q)", v)
						} else {
							value = fmt.Sprintf("%q", v)
						}

					}
				}
				g.w(value)
			}
			g.w(",\n")

		case int, *int, float64:
			g.w("\t\t\t" + k + ": ")

			typ, ok := apiRegistry[a.Request()][k]
			if !ok {
				panic(fmt.Sprintf("%s.%s: field not found", a.Request(), k))
			}

			var value string
			switch typ {
			case "string":
				value = `"` + fmt.Sprintf("%d", v) + `"`
			case "[]string":
				value = `[]string{"` + fmt.Sprintf("%d", v) + `"}`
			case "time.Duration":
				re := regexp.MustCompile("^(\\d+).*")
				value = re.ReplaceAllString(fmt.Sprintf("%d", v), "$1")
			case "*int":
				switch v.(type) {
				case int:
					g.w(`opensearchapi.IntPtr(` + fmt.Sprintf("%d", v) + `)`)
				case float64:
					if vv, ok := v.(float64); ok {
						g.w(`opensearchapi.IntPtr(` + fmt.Sprintf("%d", int(vv)) + `)`)
					}
				default:
					panic(fmt.Sprintf("Unexpected type [%T] for [%s]", v, k))
				}
			default:
				value = fmt.Sprintf("%v", v)
			}
			g.w(value)
			g.w(",\n")

		case []interface{}:
			g.w("\t\t\t" + k + ": ")

			typ, ok := apiRegistry[a.Request()][k]
			if !ok {
				panic(fmt.Sprintf("%s.%s: field not found", a.Request(), k))
			}

			switch typ {
			case "string":
				switch v.(type) {
				case string:
					g.w("`" + v.(string) + "`")
				case []interface{}:
					vvv := make([]string, 0)
					for _, vv := range v.([]interface{}) {
						vvv = append(vvv, fmt.Sprintf("%s", vv))
					}
					g.w("`" + strings.Join(vvv, ",") + "`")
				default:
					panic(fmt.Sprintf("<%s> %s{}.%s: unexpected value <%T> %#v", typ, a.Request(), k, v, v))
				}
			case "[]string":
				qv := make([]string, 0)
				for _, vv := range v.([]interface{}) {
					// TODO: Check type
					qv = append(qv, fmt.Sprintf("%q", vv.(string)))
				}
				g.w(`[]string{` + strings.Join(qv, ",") + `}`)
			case "io.Reader":
				// Serialize Bulk payloads ...
				if k == "Body" {
					var b strings.Builder
					for _, vv := range v.([]interface{}) {
						switch vv.(type) {
						case string:
							b.WriteString(vv.(string))
						default:
							j, err := json.Marshal(convert(vv))
							if err != nil {
								panic(fmt.Sprintf("%s{}.%s: %s (%s)", a.Request(), k, err, v))
							}
							b.WriteString(string(j))
						}
						b.WriteString("\n")
					}
					b.WriteString("\n")
					g.w("\t\tstrings.NewReader(`" + b.String() + "`)")
					// ... or just convert the value to JSON
				} else {
					j, err := json.Marshal(convert(v))
					if err != nil {
						panic(fmt.Sprintf("%s{}.%s: %s (%s)", a.Request(), k, err, v))
					}
					g.w("\t\tstrings.NewReader(`" + fmt.Sprintf("%s", j) + "`)")
				}
			}
			g.w(",\n")

		case map[interface{}]interface{}:
			g.w("\t\t\t" + k + ": ")
			// vv := unstash(convert(v).(map[string]interface{}))
			// fmt.Println(vv)
			j, err := json.Marshal(convert(v))
			if err != nil {
				panic(fmt.Sprintf("JSON parse error: %s; %s", err, v))
			} else {
				// Unstash values
				reStash := regexp.MustCompile(`("\$[^"]+")`)
				j = reStash.ReplaceAll(j, []byte("` + strconv.Quote(fmt.Sprintf(\"%v\", stash[$1])) + `"))

				g.w("\t\tstrings.NewReader(`" + fmt.Sprintf("%s", j) + "`)")
				g.w(",\n")
			}

		default:
			g.w(fmt.Sprintf("\t\t// TODO: %s (%v)\n", k, v))
		}
	}

	if len(a.headers) > 0 {
		if strings.Contains(a.headers["Accept"], "yaml") && strings.HasPrefix(a.Request(), "Cat") {
			g.w("\t\t" + `Format: "yaml",` + "\n")
		}

		g.w("\t\tHeader: http.Header{\n")
		for name, value := range a.headers {

			if name == "Content-Type" && value == "application/json" {
				continue
			}

			if name == "Authorization" {
				auth_fields := strings.Split(value, " ")
				auth_name := auth_fields[0]
				auth_value := auth_fields[1]
				if strings.HasPrefix(auth_value, "$") {
					auth_value = `fmt.Sprintf("%s", stash["` + strings.ReplaceAll(strings.ReplaceAll(auth_value, "{", ""), "}", "") + `"])`
				} else {
					auth_value = `"` + auth_value + `"`
				}
				g.w("\t\t\t" + `"Authorization": []string{"` + auth_name + ` " + ` + auth_value + `},` + "\n")

			} else {
				g.w("\t\t\t\"" + name + "\": []string{\"" + value + "\"},\n")
			}

		}
		g.w("\t\t},\n")
	}

	g.w("\t\t}\n\n")

	// Get response
	g.w("\t\tres, err = req.Do(context.Background(), client)\n")

	g.w(`		if err != nil {
			t.Fatalf("ERROR: %s", err)
		}
		defer res.Body.Close()
	`)

	g.w("\n\n")

	if len(a.catch) < 1 {
		// Handle error responses
		g.w(`		handleResponseError(t, res)` + "\n")
	} else {
		// TODO: Test catch
	}

	if len(skipBody) < 1 || (len(skipBody) > 0 && skipBody[0] == false) {
		// Read and parse the body
		g.w(`		handleResponseBody(t, res)` + "\n")
	}
}

func (g *Generator) genAssertion(a Assertion) {
	g.w(a.Condition())
	g.w(a.Error() + "\n")
	g.w("}\n") // Close the condition
}

func (g *Generator) genStashSet(s Stash) {
	g.w(fmt.Sprintf("// Set %q\n", s.Key()))

	value := s.Value()
	if strings.HasPrefix(value, `mapi["#`) {
		switch {
		case strings.HasPrefix(value, `mapi["#base64EncodeCredentials`):
			i, j := strings.Index(value, "("), strings.Index(value, ")")
			values := strings.Split(value[i+1:j], ",")
			value = `base64.StdEncoding.EncodeToString([]byte(`
			value += `strings.Join([]string{`
			for n, v := range values {
				value += `mapi["` + v + `"].(string)`
				if n < len(values)-1 {
					value += ","
				}
			}
			value += `}, ":")`
			value += `))`
		default:
			panic(fmt.Sprintf("Unknown transformation: %s", value))
		}
	}

	g.w(fmt.Sprintf("stash[%q] = %s\n", s.Key(), value))
}

func convert(i interface{}) interface{} {
	switch x := i.(type) {
	case map[interface{}]interface{}:
		m2 := map[string]interface{}{}
		for k, v := range x {
			var ks string
			switch k.(type) {
			case string:
				ks = k.(string)
			case int:
				ks = fmt.Sprintf("%d", k)
			default:
				ks = fmt.Sprintf("%v", k)
			}
			m2[ks] = convert(v)
		}
		return m2
	case []interface{}:
		for i, v := range x {
			x[i] = convert(v)
		}
	}
	return i
}
