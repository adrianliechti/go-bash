#   This program is free software: you can redistribute it and/or modify
#   it under the terms of the GNU General Public License as published by
#   the Free Software Foundation, either version 3 of the License, or
#   (at your option) any later version.
#
#   This program is distributed in the hope that it will be useful,
#   but WITHOUT ANY WARRANTY; without even the implied warranty of
#   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
#   GNU General Public License for more details.
#
#   You should have received a copy of the GNU General Public License
#   along with this program.  If not, see <http://www.gnu.org/licenses/>.
#
# Selected from GNU Bash 5.3 tests/case1.sub, lines 63-67, 74-77.
# Excerpts selected for go-bash on 2026-09-27.
# See README.md for provenance, adaptations, and omitted coverage.

case \x in
\x)	echo ok 9 ;;
\\x)	echo bad 9 ;;
*)	echo bad 9.1 ;;
esac

case \x in
\x)	echo mysterious 2 ;;
*)	echo oops 2 ;;
esac
