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
# Ported from GNU Bash 5.3 tests/trap.tests, lines 16, 19-24, 66-73.
# Selected 2026-09-27; commands in the excerpts are unchanged.

trap 'echo exiting' 0

# make sure a user-specified subshell runs the exit trap, but does not
# inherit the exit trap from a parent shell
( trap 'echo subshell exit' 0; exit 0 )
( exit 0 )

trap

# exit 0 in exit trap should set exit status
(
set -e
trap 'exit 0' EXIT
false   
echo bad
)
echo $?
