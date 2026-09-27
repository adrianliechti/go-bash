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
# Selected from GNU Bash 5.3 tests/case.tests, lines 14-38.
# Excerpts selected for go-bash on 2026-09-27.
# See README.md for provenance, adaptations, and omitted coverage.

case foo in
bar)	echo skip ;;
foo)	echo fallthrough ;&
bax)	echo to here ;&
qux)	echo and here;;
fop)	echo but not here;;
esac

case foobar in
bar)	echo skip ;;
foo*)	echo retest ;;&
*bar)	echo and match ;;&
qux)	echo but not this ;;
esac

case a in
a)	echo no more clauses;&
esac

x=0 y=1
case 1 in
  $((y=0)) ) ;;
  $((x=1)) ) ;&
  $((x=2)) ) echo $x.$y ;;
esac
