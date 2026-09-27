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
# Ported from GNU Bash 5.3 tests/builtins.tests, lines 75-78, 80-88.
# Selected 2026-09-27; commands in the excerpts are unchanged.

AVAR='$BVAR'
BVAR=foo

echo $AVAR

command echo $AVAR
eval echo \$AVAR
eval echo $AVAR

# test out eval with a temp environment
AVAR=bar eval echo \$AVAR
BVAR=xxx eval echo $AVAR

unset -v AVAR BVAR
