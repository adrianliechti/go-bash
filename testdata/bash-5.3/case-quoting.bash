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
# Selected from GNU Bash 5.3 tests/case1.sub, lines 14-61, 69-72.
# Excerpts selected for go-bash on 2026-09-27.
# See README.md for provenance, adaptations, and omitted coverage.

x='\x'

case x in
\x)	echo ok 1;;
*)	echo bad 1;;
esac

case x in
$x)	echo ok 2;;
*)	echo bad 2;;
esac

case $x in
\x)	echo bad 3;;
\\x)	echo ok 3 ;;
*)	echo bad 3.1 ;;
esac

case $x in
\\$x)	echo ok 4 ;;
x)	echo bad 4;;
$x)	echo bad 4.1 ;;
*)	echo bad 4.2;;
esac

case x in
\\x)	echo bad 5;;
\x)	echo ok 5;;
*)	echo bad 5.1;;
esac

case x in
\\x)	echo bad 6;;
x)	echo ok 6;;
*)	echo bad 6.1;;
esac

case x in
$x)	echo ok 7 ;;
\\$x)	echo bad 7 ;;
*)	echo bad 7.1 ;;
esac

case x in
\x)	echo ok 8 ;;
\\x)	echo bad 8 ;;
*)	echo bad 8.1 ;;
esac

case $x in
$x)	echo oops 1 ;;
*)	echo mysterious 1 ;;
esac
