package bash_test

import "testing"

func TestBashRegressionEdges(t *testing.T) {
	testBashDifferential(t, []bashTestCase{
		{"conditional and before or", `[[ x && '' || x ]]; printf '%s' "$?"; [[ x || '' && '' ]]; printf '%s' "$?"`, "00", 0},
		{"conditional nested negation", `[[ ! ! x || '' ]]; printf '%s' "$?"; [[ ! (x || '') ]]; printf '%s' "$?"; [[ ! '' && x || '' ]]; printf '%s' "$?"`, "010", 0},
		{"conditional lazy right side", `x=0; [[ ! x || x && $((x+=1)) == 1 ]]; printf '%s:%s' "$?" "$x"`, "0:1", 0},
		{"quoted loop components", `for (("i=0"; "i<3"; "i++")); do printf '%s' "$i"; done`, "012", 0},
		{"expanded loop condition", `condition='i<3'; for ((i=0; "$condition"; i++)); do printf '%s' "$i"; done`, "012", 0},
		{"literal escape boundaries", `x=a\ b; printf '<%s>' "$x"; cat <<< \~; case \é in é) printf yes;; esac`, "<a b>~\nyes", 0},
		{"literal quoted escape", `case '\x' in "\x") printf yes;; esac; case ~\/x in '~/x') printf yes;; esac`, "yesyes", 0},
		{"read whitespace remainder", `read a b <<< 'one two three\ '; printf '<%s><%s>' "$a" "$b"; read a b <<< 'one two\ '; printf '<%s><%s>' "$a" "$b"`, "<one><two three><one><two >", 0},
		{"read custom whitespace", `IFS=$'\r\f\v' read a b <<< $'\r\f\vone\rtwo\f\v'; printf '<%s><%s>' "$a" "$b"`, "<one><two>", 0},
		{"continued heredoc terminator at EOF", "cat <<EOF\nbody\nEO\\\nF", "body\n", 0},
		{"continued heredoc terminator at start", "cat <<EOF\n\\\nEOF\nprintf after", "after", 0},
		{"continued heredoc in function", "f() { cat <<EOF\nbody\nEO\\\nF\n}; f; printf after", "body\nafter", 0},
		{"continued heredoc keeps line numbers", "cat <<EOF\nbody\nEO\\\nF\nprintf '%s' \"$LINENO\"", "body\n6", 0},
		{"continued heredoc with tabs", "cat <<-EOF\n\tbody\n\tEO\\\nF\nprintf after", "body\nafter", 0},
		{"quoted heredoc keeps continuations", "cat <<'EOF'\nEO\\\nF\nEOF\nprintf after", "EO\\\nF\nafter", 0},
		{"multiple continued heredocs", "cat <<ONE <<TWO\nfirst\nON\\\nE\nsecond\nTW\\\nO\nprintf after", "second\nafter", 0},
	}, 5)
}
