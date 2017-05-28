// Copyright (c) 2016, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package syntax

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"testing"
)

func TestPrintCompact(t *testing.T) {
	t.Parallel()
	parserBash := NewParser()
	parserMirBSD := NewParser(Variant(LangMirBSDKorn))
	printer := NewPrinter()
	for i, c := range fileTests {
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			in := c.Strs[0]
			parser := parserBash
			if c.Bash == nil {
				parser = parserMirBSD
			}
			prog, err := parser.Parse(strings.NewReader(in), "")
			if err != nil {
				t.Fatalf("Unexpected error in %q: %v", in, err)
			}
			want := in
			got, err := strPrint(printer, prog)
			if err != nil {
				t.Fatalf("Unexpected error in %q: %v", in, err)
			}
			if len(got) > 0 {
				got = got[:len(got)-1]
			}
			if got != want {
				t.Fatalf("Print mismatch of %q\nwant: %q\ngot:  %q",
					in, want, got)
			}
		})
	}
}

func strPrint(p *Printer, f *File) (string, error) {
	var buf bytes.Buffer
	err := p.Print(&buf, f)
	return buf.String(), err
}

type printCase struct {
	in, want string
}

func samePrint(s string) printCase { return printCase{in: s, want: s} }

func TestPrintWeirdFormat(t *testing.T) {
	t.Parallel()
	var weirdFormats = [...]printCase{
		samePrint(`fo○ b\år`),
		samePrint(`"fo○ b\år"`),
		samePrint(`'fo○ b\år'`),
		samePrint(`${a#fo○ b\år}`),
		samePrint(`#fo○ b\år`),
		samePrint("<<EOF\nfo○ b\\år\nEOF"),
		samePrint(`$'○ b\år'`),
		samePrint("${a/b//○}"),
		// rune split by the chunking
		{strings.Repeat(" ", bufSize-1) + "○", "○"},
		// peekByte that would (but cannot) go to the next chunk
		{strings.Repeat(" ", bufSize-2) + ">(a)", ">(a)"},
		// escaped newline at end of chunk
		{"a" + strings.Repeat(" ", bufSize-2) + "\\\nb", "a \\\n\tb"},
		// panics if padding is only 4 (utf8.UTFMax)
		{strings.Repeat(" ", bufSize-10) + "${a/b//○}", "${a/b//○}"},
		// multiple p.fill calls
		{"a" + strings.Repeat(" ", bufSize*4) + "b", "a b"},
		{"foo; bar", "foo\nbar"},
		{"foo\n\n\nbar", "foo\n\nbar"},
		{"foo\n\n", "foo"},
		{"\n\nfoo", "foo"},
		{"# foo\n # bar", "# foo\n# bar"},
		samePrint("a=b # inline\nbar"),
		samePrint("a=$(b) # inline"),
		samePrint("$(a) $(b)"),
		{"if a\nthen\n\tb\nfi", "if a; then\n\tb\nfi"},
		{"if a; then\nb\nelse\nfi", "if a; then\n\tb\nfi"},
		samePrint("foo >&2 <f bar"),
		samePrint("foo >&2 bar <f"),
		{"foo >&2 bar <f bar2", "foo >&2 bar bar2 <f"},
		{"foo <<EOF bar\nl1\nEOF", "foo bar <<EOF\nl1\nEOF"},
		samePrint("foo <<\\\\\\\\EOF\nbar\n\\\\EOF"),
		samePrint("foo <<\"\\EOF\"\nbar\n\\EOF"),
		samePrint("foo <<EOF && bar\nl1\nEOF"),
		samePrint("foo <<EOF &&\nl1\nEOF\n\tbar"),
		samePrint("foo <<EOF\nl1\nEOF\n\nfoo2"),
		samePrint("<<EOF\nEOF"),
		samePrint("foo <<EOF\nEOF\n\nbar"),
		samePrint("foo <<'EOF'\nEOF\n\nbar"),
		{
			"{ foo; bar; }",
			"{\n\tfoo\n\tbar\n}",
		},
		{
			"{ foo; bar; }\n#etc",
			"{\n\tfoo\n\tbar\n}\n#etc",
		},
		{
			"{\n\tfoo; }",
			"{\n\tfoo\n}",
		},
		{
			"{ foo\n}",
			"{\n\tfoo\n}",
		},
		{
			"(foo\n)",
			"(\n\tfoo\n)",
		},
		{
			"$(foo\n)",
			"$(\n\tfoo\n)",
		},
		{
			"a\n\n\n# etc\nb",
			"a\n\n# etc\nb",
		},
		{
			"a b\\\nc d",
			"a bc \\\n\td",
		},
		{
			"a bb\\\ncc d",
			"a bbcc \\\n\td",
		},
		samePrint("a \\\n\tb \\\n\tc \\\n\t;"),
		samePrint("a=1 \\\n\tb=2 \\\n\tc=3 \\\n\t;"),
		samePrint("if a \\\n\t; then b; fi"),
		samePrint("a 'b\nb' c"),
		{
			"(foo; bar)",
			"(\n\tfoo\n\tbar\n)",
		},
		{
			"{\nfoo\nbar; }",
			"{\n\tfoo\n\tbar\n}",
		},
		samePrint("\"$foo\"\n{\n\tbar\n}"),
		{
			"{\nbar\n# extra\n}",
			"{\n\tbar\n\t# extra\n}",
		},
		{
			"foo\nbar  # extra",
			"foo\nbar # extra",
		},
		{
			"foo # 1\nfooo # 2\nfo # 3",
			"foo  # 1\nfooo # 2\nfo   # 3",
		},
		{
			" foo # 1\n fooo # 2\n fo # 3",
			"foo  # 1\nfooo # 2\nfo   # 3",
		},
		{
			"foo   # 1\nfooo  # 2\nfo    # 3",
			"foo  # 1\nfooo # 2\nfo   # 3",
		},
		{
			"fooooo\nfoo # 1\nfooo # 2\nfo # 3\nfooooo",
			"fooooo\nfoo  # 1\nfooo # 2\nfo   # 3\nfooooo",
		},
		{
			"foo\nbar\nfoo # 1\nfooo # 2",
			"foo\nbar\nfoo  # 1\nfooo # 2",
		},
		samePrint("foobar # 1\nfoo\nfoo # 2"),
		samePrint("foobar # 1\n#foo\nfoo # 2"),
		samePrint("foobar # 1\n\nfoo # 2"),
		{
			"foo # 2\nfoo2 bar # 1",
			"foo      # 2\nfoo2 bar # 1",
		},
		{
			"foo bar # 1\n! foo # 2",
			"foo bar # 1\n! foo   # 2",
		},
		{
			"aa #b\nc  #d\ne\nf #g",
			"aa #b\nc  #d\ne\nf #g",
		},
		{
			"foo; foooo # 1",
			"foo\nfoooo # 1",
		},
		{
			"aaa; b #1\nc #2",
			"aaa\nb #1\nc #2",
		},
		{
			"a #1\nbbb; c #2\nd #3",
			"a #1\nbbb\nc #2\nd #3",
		},
		samePrint("aa #c1\n{ #c2\n\tb\n}"),
		{
			"aa #c1\n{ b; c; } #c2",
			"aa #c1\n{\n\tb\n\tc\n} #c2",
		},
		samePrint("a #c1\n'b\ncc' #c2"),
		{
			"(\nbar\n# extra\n)",
			"(\n\tbar\n\t# extra\n)",
		},
		{
			"for a in 1 2\ndo\n\t# bar\ndone",
			"for a in 1 2; do\n\t# bar\ndone",
		},
		samePrint("for a in 1 2; do\n\n\tbar\ndone"),
		{
			"a \\\n\t&& b",
			"a &&\n\tb",
		},
		{
			"a \\\n\t&& b\nc",
			"a &&\n\tb\nc",
		},
		{
			"{\n(a \\\n&& b)\nc\n}",
			"{\n\t(a &&\n\t\tb)\n\tc\n}",
		},
		{
			"a && b \\\n&& c",
			"a && b &&\n\tc",
		},
		{
			"a \\\n&& $(b) && c \\\n&& d",
			"a &&\n\t$(b) && c &&\n\td",
		},
		{
			"a \\\n&& b\nc \\\n&& d",
			"a &&\n\tb\nc &&\n\td",
		},
		{
			"a \\\n&&\n#c\nb",
			"a &&\n\t#c\n\tb",
		},
		{
			"a | {\nb \\\n| c\n}",
			"a | {\n\tb |\n\t\tc\n}",
		},
		{
			"a \\\n\t&& if foo; then\nbar\nfi",
			"a &&\n\tif foo; then\n\t\tbar\n\tfi",
		},
		{
			"if\nfoo\nthen\nbar\nfi",
			"if\n\tfoo\nthen\n\tbar\nfi",
		},
		{
			"if foo \\\nbar\nthen\nbar\nfi",
			"if foo \\\n\tbar; then\n\tbar\nfi",
		},
		{
			"if foo \\\n&& bar\nthen\nbar\nfi",
			"if foo &&\n\tbar; then\n\tbar\nfi",
		},
		{
			"a |\nb |\nc",
			"a |\n\tb |\n\tc",
		},
		samePrint("a |\n\tb | c |\n\td"),
		samePrint("a | b |\n\tc |\n\td"),
		{
			"foo |\n# misplaced\nbar",
			"foo |\n\t# misplaced\n\tbar",
		},
		samePrint("{\n\tfoo\n\t#a\n\tbar\n} | etc"),
		{
			"foo &&\n#a1\n#a2\n$(bar)",
			"foo &&\n\t#a1\n\t#a2\n\t$(bar)",
		},
		{
			"{\n\tfoo\n\t#a\n} |\n# misplaced\nbar",
			"{\n\tfoo\n\t#a\n} |\n\t# misplaced\n\tbar",
		},
		samePrint("foo | bar\n#after"),
		{
			"a |\nb | #c2\nc",
			"a |\n\tb | #c2\n\tc",
		},
		{
			"{\nfoo &&\n#a1\n#a2\n$(bar)\n}",
			"{\n\tfoo &&\n\t\t#a1\n\t\t#a2\n\t\t$(bar)\n}",
		},
		{
			"foo | while read l; do\nbar\ndone",
			"foo | while read l; do\n\tbar\ndone",
		},
		samePrint("\"\\\nfoo\\\n  bar\""),
		{
			"foo \\\n>bar\netc",
			"foo \\\n\t>bar\netc",
		},
		{
			"foo \\\nfoo2 \\\n>bar",
			"foo \\\n\tfoo2 \\\n\t>bar",
		},
		samePrint("> >(foo)"),
		samePrint("x > >(foo) y"),
		samePrint("a | () |\n\tb"),
		samePrint("a | (\n\tx\n\ty\n) |\n\tb"),
		samePrint("a |\n\tif foo; then\n\t\tbar\n\tfi |\n\tb"),
		samePrint("a | if foo; then\n\tbar\nfi"),
		samePrint("a | b | if foo; then\n\tbar\nfi"),
		{
			"case $i in\n1)\nfoo\n;;\nesac",
			"case $i in\n1)\n\tfoo\n\t;;\nesac",
		},
		{
			"case $i in\n1)\nfoo\nesac",
			"case $i in\n1)\n\tfoo\n\t;;\nesac",
		},
		{
			"case $i in\n1) foo\nesac",
			"case $i in\n1) foo ;;\nesac",
		},
		{
			"case $i in\n1) foo; bar\nesac",
			"case $i in\n1)\n\tfoo\n\tbar\n\t;;\nesac",
		},
		{
			"case $i in\n1) foo; bar;;\nesac",
			"case $i in\n1)\n\tfoo\n\tbar\n\t;;\nesac",
		},
		{
			"case $i in\n1)\n#foo\n;;\nesac",
			"case $i in\n1) ;; #foo\nesac",
		},
		samePrint("case $i in\n1)\n\ta\n\t#b\n\t;;\nesac"),
		samePrint("case $i in\n1) foo() { bar; } ;;\nesac"),
		{
			"a=(\nb\nc\n) foo",
			"a=(\n\tb\n\tc\n) foo",
		},
		samePrint("a=(\n\tb #foo\n\tc #bar\n)"),
		samePrint("foo <<EOF | $(bar)\n3\nEOF"),
		{
			"a <<EOF\n$(\n\tb\n\tc)\nEOF",
			"a <<EOF\n$(\n\tb\n\tc\n)\nEOF",
		},
		{
			"( (foo) )\n$( (foo) )\n<( (foo) )",
			"( (foo))\n$( (foo))\n<((foo))",
		},
		samePrint("\"foo\n$(bar)\""),
		samePrint("\"foo\\\n$(bar)\""),
		samePrint("((foo++)) || bar"),
		{
			"a=b \\\nc=d \\\nfoo",
			"a=b \\\n\tc=d \\\n\tfoo",
		},
		{
			"a=b \\\nc=d \\\nfoo \\\nbar",
			"a=b \\\n\tc=d \\\n\tfoo \\\n\tbar",
		},
		samePrint("\"foo\nbar\"\netc"),
		samePrint("\"foo\nbar\nbar2\"\netc"),
		samePrint("a=\"$b\n\"\nd=e"),
		samePrint("\"\n\"\n\nfoo"),
		samePrint("$\"\n\"\n\nfoo"),
		samePrint("'\n'\n\nfoo"),
		samePrint("$'\n'\n\nfoo"),
		samePrint("foo <<EOF\na\nb\nc\nd\nEOF\n{\n\tbar\n}"),
		samePrint("foo bar # one\nif a; then\n\tb\nfi # two"),
		{
			"# foo\n\n\nbar",
			"# foo\n\nbar",
		},
		samePrint("#foo\n#\n#bar"),
	}

	parser := NewParser(KeepComments)
	printer := NewPrinter()
	n := 0
	for i, tc := range weirdFormats {
		check := func(t *testing.T, in, want string) {
			ioutil.WriteFile(fmt.Sprintf("../corpus/printer-%03d", n), []byte(in), 0644)
			n++
			prog, err := parser.Parse(newStrictReader(in), "")
			if err != nil {
				t.Fatalf("Unexpected error in %q: %v", in, err)
			}
			checkNewlines(t, in, prog.lines)
			got, err := strPrint(printer, prog)
			if err != nil {
				t.Fatalf("Unexpected error in %q: %v", in, err)
			}
			if got != want {
				t.Fatalf("Print mismatch:\n"+
					"in:\n%s\nwant:\n%sgot:\n%s",
					in, want, got)
			}
		}
		want := tc.want + "\n"
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			check(t, tc.in, want)
		})
		t.Run(fmt.Sprintf("%03d-nl", i), func(t *testing.T) {
			check(t, "\n"+tc.in+"\n", want)
		})
		t.Run(fmt.Sprintf("%03d-redo", i), func(t *testing.T) {
			check(t, want, want)
		})
	}
}

func parsePath(tb testing.TB, path string) *File {
	f, err := os.Open(path)
	if err != nil {
		tb.Fatal(err)
	}
	defer f.Close()
	prog, err := NewParser(KeepComments).Parse(f, "")
	if err != nil {
		tb.Fatal(err)
	}
	return prog
}

const canonicalPath = "canonical.sh"

func TestPrintMultiline(t *testing.T) {
	prog := parsePath(t, canonicalPath)
	got, err := strPrint(NewPrinter(), prog)
	if err != nil {
		t.Fatal(err)
	}

	want, err := ioutil.ReadFile(canonicalPath)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("Print mismatch in canonical.sh")
	}
}

func BenchmarkPrint(b *testing.B) {
	prog := parsePath(b, canonicalPath)
	printer := NewPrinter()
	for i := 0; i < b.N; i++ {
		if err := printer.Print(ioutil.Discard, prog); err != nil {
			b.Fatal(err)
		}
	}
}

func TestPrintSpaces(t *testing.T) {
	var spaceFormats = [...]struct {
		spaces   int
		in, want string
	}{
		{
			0,
			"{\nfoo \\\nbar\n}",
			"{\n\tfoo \\\n\t\tbar\n}",
		},
		{
			-1,
			"{\nfoo \\\nbar\n}",
			"{\nfoo \\\nbar\n}",
		},
		{
			2,
			"{\nfoo \\\nbar\n}",
			"{\n  foo \\\n    bar\n}",
		},
		{
			4,
			"{\nfoo \\\nbar\n}",
			"{\n    foo \\\n        bar\n}",
		},
	}

	parser := NewParser(KeepComments)
	for i, tc := range spaceFormats {
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			printer := NewPrinter(Indent(tc.spaces))
			prog, err := parser.Parse(strings.NewReader(tc.in), "")
			if err != nil {
				t.Fatal(err)
			}
			want := tc.want + "\n"
			got, err := strPrint(printer, prog)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("Print mismatch:\nin:\n%s\nwant:\n%sgot:\n%s",
					tc.in, want, got)
			}
		})
	}
}

var errBadWriter = fmt.Errorf("write: expected error")

type badWriter struct{}

func (b badWriter) Write(p []byte) (int, error) { return 0, errBadWriter }

func TestWriteErr(t *testing.T) {
	_ = (*byteCounter)(nil).Flush()
	f := &File{Stmts: []*Stmt{
		{
			Redirs: []*Redirect{{
				Op:   RdrOut,
				Word: litWord("foo"),
			}},
			Cmd: &Subshell{},
		},
	}}
	err := NewPrinter().Print(badWriter{}, f)
	if err == nil {
		t.Fatalf("Expected error with bad writer")
	}
	if err != errBadWriter {
		t.Fatalf("Error mismatch with bad writer:\nwant: %v\ngot:  %v",
			errBadWriter, err)
	}
}

func TestPrintBinaryNextLine(t *testing.T) {
	var tests = [...]printCase{
		{
			"foo <<EOF &&\nl1\nEOF\nbar",
			"foo <<EOF && bar\nl1\nEOF",
		},
		samePrint("a \\\n\t&& b"),
		samePrint("a \\\n\t&& b\nc"),
		{
			"{\n(a \\\n&& b)\nc\n}",
			"{\n\t(a \\\n\t\t&& b)\n\tc\n}",
		},
		{
			"a && b \\\n&& c",
			"a && b \\\n\t&& c",
		},
		{
			"a \\\n&& $(b) && c \\\n&& d",
			"a \\\n\t&& $(b) && c \\\n\t&& d",
		},
		{
			"a \\\n&& b\nc \\\n&& d",
			"a \\\n\t&& b\nc \\\n\t&& d",
		},
		{
			"a | {\nb \\\n| c\n}",
			"a | {\n\tb \\\n\t\t| c\n}",
		},
		{
			"a \\\n\t&& if foo; then\nbar\nfi",
			"a \\\n\t&& if foo; then\n\t\tbar\n\tfi",
		},
		{
			"if foo \\\n&& bar\nthen\nbar\nfi",
			"if foo \\\n\t&& bar; then\n\tbar\nfi",
		},
		{
			"a |\nb |\nc",
			"a \\\n\t| b \\\n\t| c",
		},
		{
			"foo |\n# misplaced\nbar",
			"foo \\\n\t|\n\t# misplaced\n\tbar",
		},
		samePrint("{\n\tfoo\n\t#a\n\tbar\n} | etc"),
		{
			"foo &&\n#a1\n#a2\n$(bar)",
			"foo \\\n\t&&\n\t#a1\n\t#a2\n\t$(bar)",
		},
		{
			"{\n\tfoo\n\t#a\n} |\n# misplaced\nbar",
			"{\n\tfoo\n\t#a\n} \\\n\t|\n\t# misplaced\n\tbar",
		},
		samePrint("foo | bar\n#after"),
		{
			"a |\nb | #c2\nc",
			"a \\\n\t| b \\\n\t|\n\t#c2\n\tc",
		},
	}
	parser := NewParser(KeepComments)
	printer := NewPrinter(BinaryNextLine)
	for i, tc := range tests {
		t.Run(fmt.Sprintf("%03d", i), func(t *testing.T) {
			prog, err := parser.Parse(strings.NewReader(tc.in), "")
			if err != nil {
				t.Fatal(err)
			}
			want := tc.want + "\n"
			got, err := strPrint(printer, prog)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("Print mismatch:\nin:\n%s\nwant:\n%sgot:\n%s",
					tc.in, want, got)
			}
		})
	}
}
