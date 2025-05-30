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
	"fmt"
	"strings"

	"github.com/huuvuno1/opensearch-go/v2/internal/build/utils"
)

// DebugInfo returns information about the endpoint as a string.
func (tg TestSuite) DebugInfo() string {
	var out strings.Builder

	fmt.Fprintln(&out, strings.Repeat("─", utils.TerminalWidth()))
	fmt.Fprint(&out, "["+tg.Name()+"]\n")
	fmt.Fprintln(&out, strings.Repeat("─", utils.TerminalWidth()))

	if len(tg.Setup) > 0 {
		for _, a := range tg.Setup {
			fmt.Fprint(&out, "[setup] ")
			fmt.Fprint(&out, a.Method()+"(")
			var i int
			for k, v := range a.Params() {
				i++
				fmt.Fprint(&out, ""+k+": "+fmt.Sprintf("%v", v))
				if i < len(a.Params()) {
					fmt.Fprint(&out, ", ")
				}
			}
			fmt.Fprint(&out, ")")
			fmt.Fprint(&out, "\n")
		}
	}

	if len(tg.Teardown) > 0 {
		for _, a := range tg.Teardown {
			fmt.Fprint(&out, "[tdown] ")
			fmt.Fprint(&out, a.Method()+"(")
			var i int
			for k, v := range a.Params() {
				i++
				fmt.Fprint(&out, ""+k+": "+fmt.Sprintf("%v", v))
				if i < len(a.Params()) {
					fmt.Fprint(&out, ", ")
				}
			}
			fmt.Fprint(&out, ")\n")
		}
	}

	for _, t := range tg.Tests {
		if utils.IsTTY() {
			fmt.Fprint(&out, "\x1b[1;2m")
		}
		fmt.Fprintln(&out, t.Name+":")
		if utils.IsTTY() {
			fmt.Fprint(&out, "\x1b[0;2m")
		}
		for _, a := range t.Setup {
			fmt.Fprintf(&out, "  [setup] ")
			fmt.Fprint(&out, a.Method()+"()\n")
		}
		for _, a := range t.Teardown {
			fmt.Fprintf(&out, "  [tdown] ")
			fmt.Fprint(&out, a.Method()+"()\n")
		}
		for _, a := range t.Steps {
			switch a.(type) {
			case Action:
				aa := a.(Action)
				fmt.Fprintf(&out, "  ==> ")
				fmt.Fprint(&out, aa.Method()+"(")
				var i int
				for k, v := range aa.Params() {
					i++
					fmt.Fprint(&out, ""+k+": "+fmt.Sprintf("%v", v))
					if i < len(aa.Params()) {
						fmt.Fprint(&out, ", ")
					}
				}
				fmt.Fprint(&out, ")\n")
			case Assertion:
				aa := a.(Assertion)
				fmt.Fprintf(&out, "  ~~> ")
				fmt.Fprintf(&out, "%q: %s", aa.operation, aa.payload)
				fmt.Fprint(&out, "\n")
			default:
				panic(fmt.Sprintf("Unknown step %T", a))
			}
		}
	}

	fmt.Fprintln(&out, strings.Repeat("─", utils.TerminalWidth()))
	return out.String()
}
