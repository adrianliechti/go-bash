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
# Ported from GNU Bash 5.3 tests/builtins.tests, lines 90-102.
# Selected 2026-09-27; commands in the excerpts are unchanged.

# basic umask tests
mask=$(umask)
umask 022
umask
umask -S
umask -S u=rwx,g=rwx,o=rx >/dev/null # 002
umask
umask -S
umask -p
umask -p -S
umask 0
umask -S
umask ${mask}	# restore original mask
