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
# Selected from GNU Bash 5.3 tests/cond.tests, lines 112-125, 129-144.
# Excerpts selected for go-bash on 2026-09-27.
# See README.md for provenance, adaptations, and omitted coverage.

unset IVAR A
[[ 7 -gt $IVAR ]]
echo returns: $?

[[ $IVAR -gt 7 ]]
echo returns: $?

IVAR=4
[[ $IVAR -gt 7 ]]
echo returns: $?

[[ 7 -eq 4+3 ]]
echo returns: $?


IVAR=4+3
[[ $IVAR -eq 7 ]]
echo returns: $?

A=7
[[ $IVAR -eq A ]]
echo returns: $?

[[ "$IVAR" -eq "7" ]]
echo returns: $?

A=7
[[ "$IVAR" -eq "A" ]]
echo returns: $?

unset IVAR A
