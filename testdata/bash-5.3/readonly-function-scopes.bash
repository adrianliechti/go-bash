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
# Ported from GNU Bash 5.3 tests/varenv23.sub, lines 29-34, 42-49.
# Selected 2026-09-27; commands in the excerpts are unchanged.

f() { local -r a=3; echo f:$a; }
f1() { declare -r b=3; echo f1:$b; }

a=4 f
b=4 f1
echo global1:$a $b

unset -f f f1

f() { local a=3; readonly a; echo f:$a; }
f1() { local b=3; declare -r b; echo f1:$b; }

a=4 f
b=4 f1
echo global:$a $b
