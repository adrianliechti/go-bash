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
# Ported from GNU Bash 5.3 tests/getopts.tests, lines 25-26, 29, 33-43.
# Selected 2026-09-27; commands in the excerpts are unchanged.

${THIS_SH} ./getopts1.sub -a -b bval one two three
# make sure getopts works when there are more than 9 positional parameters




# make sure that `-b bval' and `-bbval' are equivalent
${THIS_SH} ./getopts4.sub -a -b bval one two three
${THIS_SH} ./getopts4.sub -a -bbval one two three
# this tests `silent' error reporting
${THIS_SH} ./getopts4.sub -a -b
${THIS_SH} ./getopts4.sub -a -c

# make sure that `--' can be used to end the list of options
${THIS_SH} ./getopts4.sub -a -- -b bval one two three

