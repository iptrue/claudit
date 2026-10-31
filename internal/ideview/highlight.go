package ideview

import (
	"image/color"
	"path/filepath"
	"strings"
	"unicode"

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
)

var kindColor = [...]color.Color{
	plain:   theme.Code,
	keyword: theme.Keyword,
	str:     theme.String,
	comment: theme.Comment,
	number:  theme.Number,
	fn:      theme.Func,
	interp:  theme.Interp,
}

// language is what the line-by-line highlighter knows about a language family.
type language struct {
	keywords   map[string]bool
	comment    string // line comment marker; "" for none
	open, shut string // block comment delimiters, e.g. "/*" and "*/"; "" for none
	quotes     string // string delimiters
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

// sqlKeywords are written in either case.
const sqlKeywords = `add all alter and as asc begin between by case check column commit constraint create
	cross database default delete desc distinct drop else end exists false foreign from full group having if
	in index inner insert into is join key left like limit not null offset on or order outer primary
	references returning right rollback select set table then true union unique update values view when
	where with`

var (
	goLang = newLanguage(`break case chan const continue default defer else fallthrough for func go goto if
		import interface map package range return select struct switch type var true false nil iota`, "//", "/* */", "\"'`")
	rustLang = newLanguage(`as async await break const continue crate dyn else enum extern false fn for if impl in
		let loop match mod move mut pub ref return self Self static struct super trait true type unsafe use
		where while`, "//", "/* */", `"`)
	pyLang = newLanguage(`and as assert async await break class continue def del elif else except False finally
		for from global if import in is lambda None nonlocal not or pass raise return True try while with
		yield`, "#", "", `"'`)
	jsLang = newLanguage(`async await break case catch class const continue debugger default delete do else
		enum export extends false finally for from function if implements import in instanceof interface
		let new null of private protected public readonly return static super switch this throw true try
		type typeof undefined var void while yield`, "//", "/* */", "\"'`")
	shLang = newLanguage(`case do done elif else esac export fi for function if in local return then until
		while`, "#", "", `"'`)
	cLang = newLanguage(`auto bool break case char class const continue default delete do double else enum
		extern false float for goto if inline int long namespace new nullptr private protected public
		register return short signed sizeof static struct switch template this true typedef union unsigned
		using virtual void volatile while`, "//", "/* */", `"'`)
	javaLang = newLanguage(`abstract assert boolean break byte case catch char class const continue default do
		double else enum extends false final finally float for goto if implements import instanceof int
		interface long native new null package private protected public record return short static super
		switch synchronized this throw throws transient true try var void volatile while yield`, "//", "/* */", `"'`)
	kotlinLang = newLanguage(`abstract as break by catch class companion const continue data do else enum
		false final finally for fun if import in init interface internal is lateinit null object open
		operator override package private protected public return sealed super suspend this throw true try
		typealias val var when while`, "//", "/* */", `"'`)
	csLang = newLanguage(`abstract as async await base bool break byte case catch char class const continue
		decimal default delegate do double else enum event explicit extern false finally float for foreach
		get if implicit in int interface internal is lock long namespace new null object operator out
		override params private protected public readonly record ref return sealed set short static string
		struct switch this throw true try typeof uint ulong using var virtual void while yield`, "//", "/* */", `"'`)
	swiftLang = newLanguage(`as associatedtype async await break case catch class continue default defer do
		else enum extension fallthrough false fileprivate for func guard if import in init inout internal is
		let nil open operator private protocol public repeat rethrows return self Self static struct
		subscript super switch throw throws true try typealias var where while`, "//", "/* */", `"`)
	rubyLang = newLanguage(`alias and begin break case class def defined do else elsif end ensure false for if
		in module next nil not or redo rescue retry return self super then true undef unless until when
		while yield require attr_accessor attr_reader`, "#", "", `"'`)
	phpLang = newLanguage(`abstract array as break case catch class clone const continue declare default do
		echo else elseif empty enum extends false final finally fn for foreach function global if implements
		include instanceof interface isset match namespace new null private protected public readonly
		require return static switch this throw trait true try unset use var while yield`, "//", "/* */", `"'`)
	luaLang = newLanguage(`and break do else elseif end false for function goto if in local nil not or repeat
		return then true until while`, "--", "", `"'`)
	sqlLang  = newLanguage(sqlKeywords+" "+strings.ToUpper(sqlKeywords), "--", "/* */", `'"`)
	cssLang  = newLanguage(`important inherit initial unset none auto`, "", "/* */", `"'`)
	scssLang = newLanguage(`important inherit initial unset none auto if else each for mixin include extend
		use import return function`, "//", "/* */", `"'`)
	jsonLang   = newLanguage(`true false null`, "", "", `"`)
	jsoncLang  = newLanguage(`true false null`, "//", "/* */", `"`)
	yamlLang   = newLanguage(`true false null yes no on off`, "#", "", `"'`)
	tomlLang   = newLanguage(`true false`, "#", "", `"'`)
	markupLang = newLanguage(``, "", "<!-- -->", `"`) // HTML, XML, SVG
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
	".json": jsonLang, ".jsonc": jsoncLang,
	".yaml": yamlLang, ".yml": yamlLang,
	".toml": tomlLang, ".ini": tomlLang, ".cfg": tomlLang,
	".html": markupLang, ".htm": markupLang, ".xml": markupLang, ".svg": markupLang, ".plist": markupLang, ".vue": markupLang,
}

// languageNames are the languages of files known by name rather than extension, by lower-case name.
var languageNames = map[string]*language{
	"dockerfile": dockerLang, "containerfile": dockerLang,
	"makefile": shLang, "gnumakefile": shLang,
	"gemfile": rubyLang, "rakefile": rubyLang,
	".bashrc": shLang, ".zshrc": shLang, ".profile": shLang,
}

// langOf picks the language by file name or extension; nil means no colouring.
func langOf(path string) *language {
	if l := languageNames[strings.ToLower(filepath.Base(path))]; l != nil {
		return l
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
	".json": theme.Yellow, ".jsonc": theme.Yellow,
	".yaml": theme.Violet, ".yml": theme.Violet, ".toml": theme.Violet,
	".md": theme.Teal, ".markdown": theme.Teal,
	".svg": theme.Yellow, ".png": theme.Violet, ".jpg": theme.Violet,
	".jpeg": theme.Violet, ".gif": theme.Violet, ".webp": theme.Violet,
}

// fileNameColors colour files known by name, by lower-case name.
var fileNameColors = map[string]color.Color{
	"dockerfile": theme.Blue, "containerfile": theme.Blue,
	"makefile": theme.Yellow, "gnumakefile": theme.Yellow,
	".gitignore": theme.Red, ".gitattributes": theme.Red,
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
	if l == nil {
		return ks
	}
	mark := func(from, to int, k kind) {
		for i := from; i < to; i++ {
			ks[i] = k
		}
	}
	for i := 0; i < len(rs); {
		r := rs[i]
		switch {
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
			switch {
			case l == yamlLang && j < len(rs) && rs[j] == ':': // a mapping key, even on: or yes:
			case l.keywords[string(rs[i:j])]:
				mark(i, j, keyword)
			case j < len(rs) && rs[j] == '(':
				mark(i, j, fn)
			}
			i = j
		default:
			i++
		}
	}
	return ks
}

func isWord(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

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

// paintOn colours one clean line by the syntax of language l (nil: plain) on background bg: a diff's
// change keeps the colours of its code.
func paintOn(line string, l *language, bg color.Color) string {
	rs := []rune(line)
	kinds := highlight(rs, l)
	var b strings.Builder
	for i := 0; i < len(rs); {
		j := i + 1
		for j < len(rs) && kinds[j] == kinds[i] {
			j++
		}
		b.WriteString(style{kind: kinds[i], bg: bg}.render(string(rs[i:j])))
		i = j
	}
	return b.String()
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
