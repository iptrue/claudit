package ideview

import (
	"image/color"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/pixelbender/claudit/internal/theme"
)

// kind is the syntax class of a rune.
type kind uint8

const (
	plain kind = iota
	keyword
	str
	comment
	number
	fn
	interp // what a string interpolates: ${…}
	typ    // a type or a class, a tag
	// Markdown as the chat draws it, diffs as git does.
	heading // a heading, a list's bullet
	strong  // **strong**, a diff's file
	literal // `code`, a fence, a diff's hunk
	link    // [a link](url)
	faint   // a quote, a rule, a table's bars, the rest of a diff's header
	added   // a diff's added line
	removed // a diff's removed line
)

var kindColor = [...]color.Color{
	plain:   theme.Code,
	keyword: theme.Keyword,
	str:     theme.String,
	comment: theme.Comment,
	number:  theme.Number,
	fn:      theme.Func,
	interp:  theme.Interp,
	typ:     theme.Type,
	heading: theme.Accent,
	strong:  theme.Strong,
	literal: theme.Violet,
	link:    theme.Sky,
	faint:   theme.Dim,
	added:   theme.Green,
	removed: theme.Red,
}

// language is what the line-by-line highlighter knows about a language family.
type language struct {
	keywords   map[string]bool
	types      map[string]bool // the built-in types, coloured as keywords
	pascal     bool            // a PascalCase name is a type or a class, unless it is called
	tags       bool            // "<name" opens a tag, "</name" closes one: markup, JSX
	lineKey    *regexp.Regexp  // finds a line's key, its first group, in the colour of keywords: YAML, TOML
	colonKey   bool            // a string or a name a ":" follows is a key, in the colour of keywords: JSON
	comment    string          // line comment marker; "" for none
	open, shut string          // block comment delimiters, e.g. "/*" and "*/"; "" for none
	quotes     string          // string delimiters
}

// newLanguage builds a language; block is its block comment delimiters separated by a space ("/* */").
func newLanguage(keywords, comment, block, quotes string) *language {
	l := &language{keywords: map[string]bool{}, comment: comment, quotes: quotes}
	l.open, l.shut, _ = strings.Cut(block, " ")
	for _, k := range strings.Fields(keywords) {
		l.keywords[k] = true
	}
	return l
}

// typed gives l its built-in types and tells whether its PascalCase names are types.
func (l *language) typed(types string, pascal bool) *language {
	l.types, l.pascal = map[string]bool{}, pascal
	for _, t := range strings.Fields(types) {
		l.types[t] = true
	}
	return l
}

// tagged has l tell tags: markup, JSX.
func (l *language) tagged() *language {
	l.tags = true
	return l
}

// keyedBy has l colour the key re finds on a line, its first group, as a keyword.
func (l *language) keyedBy(re *regexp.Regexp) *language {
	l.lineKey = re
	return l
}

// keyedByColon has l colour a string or a name a ":" follows as a keyword: an object's key.
func (l *language) keyedByColon() *language {
	l.colonKey = true
	return l
}

// typeIntros are the keywords that a name of a type follows: "type T", "class C", "new C(…)".
var typeIntros = map[string]bool{
	"type": true, "class": true, "struct": true, "interface": true, "enum": true, "trait": true,
	"union": true, "record": true, "object": true, "protocol": true, "extension": true, "impl": true,
	"typealias": true, "module": true, "extends": true, "implements": true, "new": true,
}

// sqlKeywords are written in either case.
const sqlKeywords = `add all alter and as asc begin between by case check column commit constraint create
	cross database default delete desc distinct drop else end exists false foreign from full group having if
	in index inner insert into is join key left like limit not null offset on or order outer primary
	references returning right rollback select set table then true union unique update values view when
	where with`

var (
	goLang = newLanguage(`break case chan const continue default defer else fallthrough for func go goto if
		import interface map package range return select struct switch type var true false nil iota`, "//", "/* */", "\"'`").
		typed(`any bool byte comparable complex64 complex128 error float32 float64 int int8 int16 int32 int64
			rune string uint uint8 uint16 uint32 uint64 uintptr`, false) // exported names of every sort are PascalCase
	rustLang = newLanguage(`as async await break const continue crate dyn else enum extern false fn for if impl in
		let loop match mod move mut pub ref return self Self static struct super trait true type unsafe use
		where while`, "//", "/* */", `"`).
		typed(`bool char f32 f64 i8 i16 i32 i64 i128 isize str u8 u16 u32 u64 u128 usize`, true)
	pyLang = newLanguage(`and as assert async await break class continue def del elif else except False finally
		for from global if import in is lambda None nonlocal not or pass raise return True try while with
		yield`, "#", "", `"'`).
		typed(`bool bytearray bytes complex dict float frozenset int list object set str tuple`, true)
	jsLang = newLanguage(`async await break case catch class const continue debugger default delete do else
		enum export extends false finally for from function if implements import in instanceof interface
		let new null of private protected public readonly return static super switch this throw true try
		type typeof undefined var void while yield`, "//", "/* */", "\"'`").
		typed(`any bigint boolean never number object string symbol unknown`, true).tagged()
	shLang = newLanguage(`case do done elif else esac export fi for function if in local return then until
		while`, "#", "", `"'`)
	cLang = newLanguage(`auto break case class const continue default delete do else enum extern false for goto
		if inline namespace new nullptr private protected public register return sizeof static struct switch
		template this true typedef union using virtual volatile while`, "//", "/* */", `"'`).
		typed(`bool char double float int long short signed unsigned void size_t ssize_t int8_t int16_t
			int32_t int64_t uint8_t uint16_t uint32_t uint64_t`, true)
	javaLang = newLanguage(`abstract assert break case catch class const continue default do else enum extends
		false final finally for goto if implements import instanceof interface native new null package
		private protected public record return static super switch synchronized this throw throws transient
		true try var volatile while yield`, "//", "/* */", `"'`).
		typed(`boolean byte char double float int long short void`, true)
	kotlinLang = newLanguage(`abstract as break by catch class companion const continue data do else enum
		false final finally for fun if import in init interface internal is lateinit null object open
		operator override package private protected public return sealed super suspend this throw true try
		typealias val var when while`, "//", "/* */", `"'`).
		typed(``, true)
	csLang = newLanguage(`abstract as async await base break case catch class const continue default delegate
		do else enum event explicit extern false finally for foreach get if implicit in interface internal
		is lock namespace new null operator out override params private protected public readonly record ref
		return sealed set static struct switch this throw true try typeof using var virtual while yield`,
		"//", "/* */", `"'`).
		typed(`bool byte char decimal double float int long object short string uint ulong void`, true)
	swiftLang = newLanguage(`as associatedtype async await break case catch class continue default defer do
		else enum extension fallthrough false fileprivate for func guard if import in init inout internal is
		let nil open operator private protocol public repeat rethrows return self Self static struct
		subscript super switch throw throws true try typealias var where while`, "//", "/* */", `"`).
		typed(``, true)
	rubyLang = newLanguage(`alias and begin break case class def defined do else elsif end ensure false for if
		in module next nil not or redo rescue retry return self super then true undef unless until when
		while yield require attr_accessor attr_reader`, "#", "", `"'`).
		typed(``, true)
	phpLang = newLanguage(`abstract array as break case catch class clone const continue declare default do
		echo else elseif empty enum extends false final finally fn for foreach function global if implements
		include instanceof interface isset match namespace new null private protected public readonly
		require return static switch this throw trait true try unset use var while yield`, "//", "/* */", `"'`).
		typed(`bool float int mixed string void`, true)
	luaLang = newLanguage(`and break do else elseif end false for function goto if in local nil not or repeat
		return then true until while`, "--", "", `"'`)
	sqlLang  = newLanguage(sqlKeywords+" "+strings.ToUpper(sqlKeywords), "--", "/* */", `'"`)
	cssLang  = newLanguage(`important inherit initial unset none auto`, "", "/* */", `"'`)
	scssLang = newLanguage(`important inherit initial unset none auto if else each for mixin include extend
		use import return function`, "//", "/* */", `"'`)
	jsonLang   = newLanguage(`true false null`, "", "", `"`).keyedByColon()
	jsoncLang  = newLanguage(`true false null`, "//", "/* */", `"`).keyedByColon()
	yamlLang   = newLanguage(`true false null yes no on off`, "#", "", `"'`).keyedBy(yamlKey)
	tomlLang   = newLanguage(`true false`, "#", "", `"'`).keyedBy(iniKey) // INI, .env, .properties too
	markupLang = newLanguage(``, "", "<!-- -->", `"`).tagged()            // HTML, XML, SVG
	mdLang     = newLanguage(``, "", "", ``)
	diffLang   = newLanguage(``, "", "", ``)
	ignoreLang = newLanguage(``, "#", "", ``) // .gitignore and the like
	csvLang    = newLanguage(``, "", "", `"`)
	goModLang  = newLanguage(`module go require replace exclude retract toolchain godebug tool`, "//", "", "\"`")
	protoLang  = newLanguage(`syntax edition package import option message enum service rpc returns stream
		repeated optional required map oneof reserved extend extensions to max true false weak public`, "//", "/* */", `"'`).
		typed(`double float int32 int64 uint32 uint64 sint32 sint64 fixed32 fixed64 sfixed32 sfixed64 bool
			string bytes`, true)
	graphqlLang = newLanguage(`query mutation subscription fragment on type interface union enum input scalar
		schema extend implements directive repeatable true false null`, "#", "", `"`).
		typed(`Int Float String Boolean ID`, true)
	hclLang = newLanguage(`resource data variable output module provider locals terraform moved import check
		dynamic content for_each count depends_on lifecycle for in if else endif endfor true false null`, "#", "/* */", `"`).
		typed(`string number bool list map set object tuple any`, false)
	dockerLang = newLanguage(`FROM AS RUN CMD ENTRYPOINT COPY ADD ENV ARG WORKDIR EXPOSE VOLUME USER LABEL
		HEALTHCHECK SHELL ONBUILD STOPSIGNAL`, "#", "", `"'`)
)

var languages = map[string]*language{
	".go": goLang,
	".rs": rustLang,
	".py": pyLang,
	".js": jsLang, ".jsx": jsLang, ".mjs": jsLang, ".cjs": jsLang, ".ts": jsLang, ".tsx": jsLang,
	".sh": shLang, ".bash": shLang, ".zsh": shLang,
	".c": cLang, ".h": cLang, ".cc": cLang, ".cpp": cLang, ".cxx": cLang, ".hpp": cLang, ".m": cLang,
	".java": javaLang, ".scala": javaLang, ".groovy": javaLang, ".dart": javaLang,
	".kt": kotlinLang, ".kts": kotlinLang, ".cs": csLang, ".swift": swiftLang,
	".rb": rubyLang, ".rake": rubyLang, ".php": phpLang, ".lua": luaLang, ".sql": sqlLang,
	".css": cssLang, ".scss": scssLang, ".less": scssLang,
	".json": jsonLang, ".jsonc": jsoncLang, ".jsonl": jsonLang, ".json5": jsoncLang,
	".geojson": jsonLang, ".webmanifest": jsonLang, ".ipynb": jsonLang,
	".md": mdLang, ".markdown": mdLang, ".mdx": mdLang,
	".diff": diffLang, ".patch": diffLang, ".csv": csvLang, ".tsv": csvLang,
	".gitignore": ignoreLang, ".dockerignore": ignoreLang, ".npmignore": ignoreLang, ".prettierignore": ignoreLang,
	".eslintignore": ignoreLang, ".gitattributes": ignoreLang,
	".proto": protoLang, ".graphql": graphqlLang, ".gql": graphqlLang, ".graphqls": graphqlLang,
	".tf": hclLang, ".tfvars": hclLang, ".hcl": hclLang, ".mod": goModLang,
	".properties": tomlLang, ".conf": tomlLang, ".cnf": tomlLang, ".service": tomlLang,
	".mk": shLang, ".fish": shLang, ".ksh": shLang,
	".xhtml": markupLang, ".xsd": markupLang, ".xsl": markupLang, ".xslt": markupLang, ".xaml": markupLang,
	".csproj": markupLang, ".fsproj": markupLang, ".props": markupLang, ".targets": markupLang, ".resx": markupLang,
	".svelte": markupLang, ".astro": markupLang,
	".yaml": yamlLang, ".yml": yamlLang,
	".toml": tomlLang, ".ini": tomlLang, ".cfg": tomlLang,
	".html": markupLang, ".htm": markupLang, ".xml": markupLang, ".svg": markupLang, ".plist": markupLang, ".vue": markupLang,
}

// languageNames are the languages of files known by name rather than extension, by lower-case name.
var languageNames = map[string]*language{
	"dockerfile": dockerLang, "containerfile": dockerLang,
	"makefile": shLang, "gnumakefile": shLang,
	"gemfile": rubyLang, "rakefile": rubyLang,
	".bashrc": shLang, ".zshrc": shLang, ".profile": shLang, ".bash_profile": shLang, ".bash_aliases": shLang,
	".bash_logout": shLang, ".zprofile": shLang, ".zshenv": shLang, ".zlogin": shLang, ".envrc": shLang,
	"vagrantfile": rubyLang, "brewfile": rubyLang, "podfile": rubyLang, "fastfile": rubyLang,
	"jenkinsfile": javaLang, "go.work": goModLang, "codeowners": ignoreLang, "pipfile": tomlLang,
	".editorconfig": tomlLang, ".gitconfig": tomlLang, ".gitmodules": tomlLang, ".npmrc": tomlLang,
	".babelrc": jsoncLang, ".eslintrc": jsoncLang, ".prettierrc": jsoncLang, ".swcrc": jsoncLang,
}

// langOf picks the language by file name or extension; nil means no colouring.
func langOf(path string) *language {
	base := strings.ToLower(filepath.Base(path))
	if l := languageNames[base]; l != nil {
		return l
	}
	if base == ".env" || strings.HasPrefix(base, ".env.") { // .env.local and the like
		return tomlLang
	}
	return languages[strings.ToLower(filepath.Ext(path))]
}

// fileColors colour file names in the explorer and in command output by extension: each
// language's brand colour as the palette has it.
var fileColors = map[string]color.Color{
	".go": theme.Cyan, ".mod": theme.Pink, ".sum": theme.Pink,
	".rs": theme.Orange,
	".py": theme.Yellow,
	".js": theme.Yellow, ".jsx": theme.Yellow, ".mjs": theme.Yellow, ".cjs": theme.Yellow,
	".ts": theme.Blue, ".tsx": theme.Blue,
	".sh": theme.Green, ".bash": theme.Green, ".zsh": theme.Green,
	".c": theme.Blue, ".h": theme.Blue,
	".cc": theme.Pink, ".cpp": theme.Pink, ".cxx": theme.Pink, ".hpp": theme.Pink,
	".java": theme.Orange, ".kt": theme.Violet, ".kts": theme.Violet,
	".cs": theme.Green, ".swift": theme.Red,
	".rb": theme.Red, ".php": theme.Violet, ".lua": theme.Blue, ".sql": theme.Yellow,
	".html": theme.Orange, ".htm": theme.Orange, ".xml": theme.Orange,
	".css": theme.Blue, ".scss": theme.Pink, ".less": theme.Pink,
	".json": theme.Yellow, ".jsonc": theme.Yellow, ".jsonl": theme.Yellow, ".json5": theme.Yellow, ".geojson": theme.Yellow,
	".ipynb": theme.Orange, ".mdx": theme.Teal, ".diff": theme.Green, ".patch": theme.Green,
	".csv": theme.Green, ".tsv": theme.Green, ".proto": theme.Blue, ".graphql": theme.Pink, ".gql": theme.Pink,
	".tf": theme.Violet, ".tfvars": theme.Violet, ".hcl": theme.Violet,
	".ini": theme.Violet, ".cfg": theme.Violet, ".conf": theme.Violet, ".properties": theme.Violet,
	".xsd": theme.Orange, ".xsl": theme.Orange, ".xaml": theme.Orange, ".csproj": theme.Orange,
	".svelte": theme.Orange, ".astro": theme.Orange,
	".yaml": theme.Violet, ".yml": theme.Violet, ".toml": theme.Violet,
	".md": theme.Teal, ".markdown": theme.Teal,
	".svg": theme.Yellow, ".png": theme.Violet, ".jpg": theme.Violet,
	".jpeg": theme.Violet, ".gif": theme.Violet, ".webp": theme.Violet,
}

// fileNameColors colour files known by name, by lower-case name.
var fileNameColors = map[string]color.Color{
	"dockerfile": theme.Blue, "containerfile": theme.Blue,
	"makefile": theme.Yellow, "gnumakefile": theme.Yellow,
	".gitignore": theme.Red, ".gitattributes": theme.Red, "go.work": theme.Pink,
}

// fileColor is the colour of a file's name by its type; nil for an unknown type.
func fileColor(path string) color.Color {
	if c := fileNameColors[strings.ToLower(filepath.Base(path))]; c != nil {
		return c
	}
	return fileColors[strings.ToLower(filepath.Ext(path))]
}

// highlight classifies each rune of one line. Lines are independent: a block comment or string
// that spans lines is coloured only on the line where it starts.
func highlight(rs []rune, l *language) []kind {
	ks := make([]kind, len(rs))
	switch l {
	case nil:
		return ks
	case mdLang:
		return markdown(rs)
	case diffLang:
		return diffKinds(rs)
	case ignoreLang:
		return ignoreKinds(rs)
	}
	mark := func(from, to int, k kind) {
		for i := from; i < to; i++ {
			ks[i] = k
		}
	}
	k0, k1 := -1, -1 // the line's key, whole
	if s := string(rs); l.lineKey != nil {
		if m := l.lineKey.FindStringSubmatchIndex(s); m != nil {
			k0, k1 = utf8.RuneCountInString(s[:m[2]]), utf8.RuneCountInString(s[:m[3]])
		}
	}
	for i := 0; i < len(rs); {
		r := rs[i]
		switch {
		case i == k0: // in the colour of keywords, "on:" and "yes:" too
			mark(k0, k1, keyword)
			i = k1
		// A shell "#" starts a comment only at the start of a word ($#, ${#a} are not comments).
		case l.comment != "" && hasPrefix(rs[i:], l.comment) && (l.comment != "#" || i == 0 || unicode.IsSpace(rs[i-1])):
			mark(i, len(rs), comment)
			return ks
		case l.open != "" && hasPrefix(rs[i:], l.open):
			j := i + len([]rune(l.open))
			for j < len(rs) && !hasPrefix(rs[j:], l.shut) {
				j++
			}
			j = min(j+len([]rune(l.shut)), len(rs))
			mark(i, j, comment)
			i = j
		case strings.ContainsRune(l.quotes, r):
			j := i + 1
			var holes [][2]int // the ${…} in it
			for j < len(rs) && rs[j] != r {
				switch {
				case rs[j] == '\\':
					j++
				case hasPrefix(rs[j:], "${"):
					k := j
					for k < len(rs) && rs[k] != '}' {
						k++
					}
					holes, j = append(holes, [2]int{j, min(k+1, len(rs))}), k
				}
				j++
			}
			j = min(j+1, len(rs))
			mark(i, j, str)
			for _, h := range holes {
				mark(h[0], h[1], interp)
			}
			if l.colonKey && colonAfter(rs, j) {
				mark(i, j, keyword)
			}
			i = j
		case unicode.IsDigit(r):
			j := i + 1
			for j < len(rs) && (isWord(rs[j]) || rs[j] == '.') {
				j++
			}
			mark(i, j, number)
			i = j
		case isWord(r):
			j := i + 1
			for j < len(rs) && isWord(rs[j]) {
				j++
			}
			w, last := string(rs[i:j]), !qualifies(rs, j) // last of its qualified name: not "pkg" of "pkg.T"
			switch {
			case l.keywords[w], l.types[w], l.colonKey && colonAfter(rs, j): // built-in types as keywords
				mark(i, j, keyword)
			case last && introduced(rs, chainStart(rs, i), l):
				mark(i, j, typ)
			case j < len(rs) && rs[j] == '(':
				mark(i, j, fn)
			case l.pascal && isPascal(rs[i:j]), l == goLang && last && goType(rs, chainStart(rs, i), j, unicode.IsUpper(r)):
				mark(i, j, typ)
			}
			i = j
		case l == tomlLang && r == '[' && strings.TrimSpace(string(rs[:i])) == "": // a section: [server], [[bin]]
			j := find(rs, i, "]")
			if j < 0 {
				j = len(rs) - 1
			}
			for j+1 < len(rs) && rs[j+1] == ']' {
				j++
			}
			mark(i, j+1, typ)
			i = j + 1
		case r == '<' && l.tags && (l == markupLang || i == 0 || !isWord(rs[i-1]) && rs[i-1] != ')' && rs[i-1] != ']'):
			j := i + 1 // the name, past "<" or "</"; in JSX "a<b" compares
			if j < len(rs) && rs[j] == '/' {
				j++
			}
			k := j
			for k < len(rs) && (isWord(rs[k]) || k > j && strings.ContainsRune("-:.", rs[k])) {
				k++
			}
			if k > j && !unicode.IsDigit(rs[j]) {
				mark(j, k, typ)
			}
			i = max(k, i+1)
		default:
			i++
		}
	}
	return ks
}

// iniKey finds a setting's key in TOML, INI, .env and .properties: "name =", "a.b-c=", `"a b" =`,
// the "PORT" of "export PORT=1", "log.level: "; not a section, a comment, or the "http" of "http://".
var iniKey = regexp.MustCompile(`^\s*(?:export\s+)?("[^"]*"|'[^']*'|[^\s#;=:\[\]"'][^=:#]*?)\s*(?:=|:(?:\s|$))`)

// colonAfter reports whether a ":" follows rs[:j] past blanks.
func colonAfter(rs []rune, j int) bool {
	for j < len(rs) && unicode.IsSpace(rs[j]) {
		j++
	}
	return j < len(rs) && rs[j] == ':'
}

// yamlKey finds a YAML mapping's key: "name:", "runs-on:", "- uses:", "'a b':", the "url" of
// "url: http://x:80"; not a list's item, a comment, or a colon without a blank after it.
var yamlKey = regexp.MustCompile(`^\s*(?:-\s+)*("[^"]*"|'[^']*'|[^\s#'"][^:#]*?)\s*:(?:\s|$)`)

var (
	mdHeading = regexp.MustCompile(`^#{1,6}(\s|$)`)
	mdItem    = regexp.MustCompile(`^([-*+]|\d{1,3}[.)])\s`)
	mdRule    = regexp.MustCompile(`^([-*_])(\s*[-*_]){2,}$`)
)

// markdown classifies the runes of one line of markdown as the chat draws them: a heading and a
// list's bullet in the accent, **strong**, `code` and a fence, [links], and dim a quote, a rule
// and a table's bars. Its markers keep the colour of what they mark.
func markdown(rs []rune) []kind {
	ks := make([]kind, len(rs))
	mark := func(from, to int, k kind) {
		for i := from; i < to; i++ {
			ks[i] = k
		}
	}
	s := 0
	for s < len(rs) && unicode.IsSpace(rs[s]) {
		s++
	}
	t := strings.TrimSpace(string(rs))
	switch {
	case strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~"):
		mark(s, len(rs), literal)
		return ks
	case mdHeading.MatchString(t):
		mark(s, len(rs), heading)
		return ks
	case strings.HasPrefix(t, ">"), mdRule.MatchString(t), strings.HasPrefix(t, "|") && strings.Trim(t, "|:- ") == "":
		mark(s, len(rs), faint)
		return ks
	}
	if m := mdItem.FindStringSubmatch(t); m != nil { // ASCII: its bytes are its runes
		mark(s, s+len(m[1]), heading)
		s += len(m[1])
	}
	table := s < len(rs) && rs[s] == '|'
	for i := s; i < len(rs); i++ {
		switch {
		case rs[i] == '`':
			if j := find(rs, i+1, "`"); j > i+1 {
				mark(i, j+1, literal)
				i = j
			}
		case hasPrefix(rs[i:], "**"):
			if j := find(rs, i+2, "**"); j > i+2 {
				mark(i, j+2, strong)
				i = j + 1
			}
		case rs[i] == '[':
			if j := find(rs, i+1, "]("); j > i {
				if k := find(rs, j+2, ")"); k > 0 {
					mark(i, k+1, link)
					i = k
				}
			}
		case rs[i] == '|' && table:
			ks[i] = faint
		}
	}
	return ks
}

// diffMeta are the lines of a diff's header past its first, grey as git draws them.
var diffMeta = []string{"index ", "new file mode", "deleted file mode", "old mode", "new mode", "similarity index",
	"dissimilarity index", "rename from", "rename to", "copy from", "copy to", "Binary files", "+++ ", "--- "}

// diffKinds classifies a line of a diff, whole, as git colours it: a file's or a commit's header,
// the rest of a header grey, a hunk's head, what is added, what is removed; the lines around them
// plain.
func diffKinds(rs []rune) []kind {
	line, k := string(rs), plain
	switch {
	case strings.HasPrefix(line, "diff ") || strings.HasPrefix(line, "commit "):
		k = strong
	case slices.ContainsFunc(diffMeta, func(p string) bool { return strings.HasPrefix(line, p) }):
		k = faint
	case strings.HasPrefix(line, "@@"):
		k = literal
	case strings.HasPrefix(line, "+"):
		k = added
	case strings.HasPrefix(line, "-"):
		k = removed
	}
	ks := make([]kind, len(rs))
	for i := range ks {
		ks[i] = k
	}
	return ks
}

// ignoreKinds classifies a line of an ignore list (.gitignore and the like): a comment; a
// negation's "!" and the globs as keywords; the paths plain.
func ignoreKinds(rs []rune) []kind {
	ks := make([]kind, len(rs))
	for i, r := range rs {
		switch {
		case r == '#' && strings.TrimSpace(string(rs[:i])) == "":
			for j := i; j < len(rs); j++ {
				ks[j] = comment
			}
			return ks
		case strings.ContainsRune("*?[]", r), r == '!' && strings.TrimSpace(string(rs[:i])) == "":
			ks[i] = keyword
		}
	}
	return ks
}

// find is where sub starts in rs at or after from; -1 for nowhere.
func find(rs []rune, from int, sub string) int {
	for i := from; i < len(rs); i++ {
		if hasPrefix(rs[i:], sub) {
			return i
		}
	}
	return -1
}

func isWord(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

// isPascal reports whether a word is PascalCase: a capital first, a small letter after it ("Foo",
// "HTTPServer"; not "T", "ID", "MAX_N").
func isPascal(w []rune) bool {
	return unicode.IsUpper(w[0]) && slices.ContainsFunc(w[1:], unicode.IsLower)
}

// qualifies reports whether the word that ends at j goes on into a qualified name: "pkg" of
// "pkg.T", "a" of "a::B".
func qualifies(rs []rune, j int) bool {
	switch {
	case j < len(rs) && rs[j] == '.':
		j++
	case hasPrefix(rs[j:], "::"):
		j += 2
	default:
		return false
	}
	return j < len(rs) && isWord(rs[j]) && !unicode.IsDigit(rs[j])
}

// chainStart is where the qualified name whose last word starts at i begins: back over "pkg." and "a::".
func chainStart(rs []rune, i int) int {
	for {
		k := i
		switch {
		case k > 0 && rs[k-1] == '.':
			k--
		case k > 1 && rs[k-1] == ':' && rs[k-2] == ':':
			k -= 2
		}
		if k == i || k == 0 || !isWord(rs[k-1]) {
			return i
		}
		for k > 0 && isWord(rs[k-1]) {
			k--
		}
		i = k
	}
}

// wordBefore is the word before rs[s] with blanks between them: "" when there are none, or no word.
func wordBefore(rs []rune, s int) string {
	k := s
	for k > 0 && unicode.IsSpace(rs[k-1]) {
		k--
	}
	e := k
	for k > 0 && isWord(rs[k-1]) {
		k--
	}
	if e == s {
		return ""
	}
	return string(rs[k:e])
}

// introduced reports whether the name that starts at s follows a keyword of l that names a type
// after it: "type T", "class C", "new C(…)".
func introduced(rs []rune, s int, l *language) bool {
	w := wordBefore(rs, s)
	return typeIntros[w] && l.keywords[w]
}

// goType reports whether the name that starts at s and ends at j stands where Go puts a type: before
// a composite literal's "{", or after what goTypeAt takes. A pointer's "*T" is a type where "*" is,
// or when T is capital: "*p" dereferences, "2*time.Second" multiplies.
func goType(rs []rune, s, j int, capital bool) bool {
	if j < len(rs) && rs[j] == '{' || goTypeAt(rs, s, j) {
		return true
	}
	if s == 0 || rs[s-1] != '*' || s > 1 && (isWord(rs[s-2]) || rs[s-2] == ')') {
		return false
	}
	return capital || goTypeAt(rs, s-1, j)
}

// goTypeAt reports whether what comes before rs[s] has a type that ends at j follow it in Go: "]"
// ("[]T", "map[K]V"), "map[", "...", ".(", "chan", a result's ")" ("func() T"), or a name declared
// first in a statement, a parameter or a field ("x T", "a, b T", "var x T") when no word follows the
// type: in prose every other word would be one.
func goTypeAt(rs []rune, s, j int) bool {
	k := s
	for k > 0 && unicode.IsSpace(rs[k-1]) {
		k--
	}
	if k == 0 {
		return false
	}
	if k == s {
		switch rs[k-1] {
		case ']':
			return true
		case '[':
			return hasSuffix(rs[:k-1], "map")
		case '(':
			return k > 1 && rs[k-2] == '.'
		case '.':
			return hasSuffix(rs[:k], "...")
		}
		return false
	}
	w := wordBefore(rs, s)
	switch {
	case rs[k-1] == ')' || w == "chan":
		return true
	case w == "" || goLang.keywords[w] || unicode.IsDigit(rune(w[0])) || wordAfter(rs, j):
		return false
	}
	b := k - utf8.RuneCountInString(w) // where the declared name starts
	for k = b; k > 0 && unicode.IsSpace(rs[k-1]); k-- {
	}
	return k == 0 || strings.ContainsRune("(,{;", rs[k-1]) || goLang.keywords[wordBefore(rs, b)]
}

// wordAfter reports whether a word follows rs[:j] past blanks.
func wordAfter(rs []rune, j int) bool {
	k := j
	for k < len(rs) && unicode.IsSpace(rs[k]) {
		k++
	}
	return k > j && k < len(rs) && isWord(rs[k])
}

func hasSuffix(rs []rune, s string) bool {
	n := utf8.RuneCountInString(s)
	return len(rs) >= n && hasPrefix(rs[len(rs)-n:], s)
}

func hasPrefix(rs []rune, p string) bool {
	for _, r := range p {
		if len(rs) == 0 || rs[0] != r {
			return false
		}
		rs = rs[1:]
	}
	return true
}

// fenceExt maps the language names of markdown code fences to file names langOf knows.
var fenceExt = map[string]string{
	"go": ".go", "golang": ".go", "rust": ".rs", "rs": ".rs", "python": ".py", "py": ".py",
	"javascript": ".js", "js": ".js", "typescript": ".ts", "ts": ".ts", "tsx": ".tsx", "jsx": ".jsx",
	"sh": ".sh", "bash": ".sh", "shell": ".sh", "zsh": ".sh", "console": ".sh",
	"c": ".c", "cpp": ".cpp", "c++": ".cpp", "h": ".h", "objc": ".m", "objective-c": ".m",
	"java": ".java", "scala": ".scala", "groovy": ".groovy", "dart": ".dart",
	"kotlin": ".kt", "kt": ".kt", "csharp": ".cs", "cs": ".cs", "c#": ".cs", "swift": ".swift",
	"ruby": ".rb", "rb": ".rb", "php": ".php", "lua": ".lua", "sql": ".sql",
	"css": ".css", "scss": ".scss", "less": ".less",
	"json": ".json", "jsonc": ".jsonc", "yaml": ".yaml", "yml": ".yaml", "toml": ".toml", "ini": ".ini",
	"html": ".html", "xml": ".xml", "svg": ".svg", "vue": ".vue",
	"dockerfile": "Dockerfile", "docker": "Dockerfile", "makefile": "Makefile", "make": "Makefile",
	"diff": ".diff", "patch": ".diff", "markdown": ".md", "md": ".md", "mdx": ".md",
	"jsonl": ".jsonl", "json5": ".json5", "csv": ".csv", "tsv": ".csv", "gitignore": ".gitignore",
	"dotenv": ".env", "env": ".env", "properties": ".properties", "conf": ".conf",
	"proto": ".proto", "protobuf": ".proto", "graphql": ".graphql", "gql": ".graphql",
	"hcl": ".hcl", "terraform": ".tf", "tf": ".tf", "gomod": ".mod", "fish": ".sh", "svelte": ".svelte",
}

// Highlight colours one line of code in the language of a markdown fence ("go", "python", …);
// an unknown language leaves it plain. line must be clean: no tabs or control characters.
func Highlight(line, lang string) string {
	return paint(line, langOf(fenceExt[strings.ToLower(lang)]))
}

// paint colours one clean line by the syntax of language l; nil leaves it plain.
func paint(line string, l *language) string {
	rs := []rune(line)
	return paintRunes(rs, highlight(rs, l), nil)
}

// paintRunes draws rs in the colours of their kinds; a colour in fg, when there is one, replaces
// that of its rune.
func paintRunes(rs []rune, kinds []kind, fg []color.Color) string {
	at := func(i int) style {
		if fg == nil {
			return style{kind: kinds[i]}
		}
		return style{kind: kinds[i], fg: fg[i]}
	}
	var b strings.Builder
	for i := 0; i < len(rs); {
		j := i + 1
		for j < len(rs) && at(j) == at(i) {
			j++
		}
		b.WriteString(at(i).render(string(rs[i:j])))
		i = j
	}
	return b.String()
}
