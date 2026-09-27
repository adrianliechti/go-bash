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
# Selected from GNU Bash 5.3 tests/heredoc.tests, lines 14-48, 64-94, 107-134, 174-176.
# Excerpts selected for go-bash on 2026-09-27.
# See README.md for provenance, adaptations, and omitted coverage.

# basics
cat <<EOF
a
b
c
EOF
read x <<EOF
a
b
c
EOF
echo "$x"
read x y <<\EOF
$PS4
EOF
echo "$x"

# empty here-documents
read x <<EOF
EOF
echo "$x"
read x <<\EOF
EOF
echo "$x"
read x <<EOF
$empty
EOF
echo "$x"

# check order and content of multiple here docs
cat << EOF1 << EOF2 
hi
EOF1
there
EOF2

# check quoted here-doc is protected

a=foo
cat << 'EOF'
hi\
there$a
stuff
EOF

# check that quoted here-documents don't have \newline processing done

cat << 'EOF'
hi\
there
EO\
F
EOF
true

# but unquoted here-documents remove backslash-newline
cat <<EOF
line 1\
line 2
EOF

# check that \newline is removed at start of here-doc
cat << EO\
F
hi
EOF


# check operation of tab removal in here documents
cat <<- EOF
	tab 1
	tab 2
	tab 3
	EOF

# check appending of text to file from here document
rm -f ${TMPDIR}/bash-zzz-$$
cat > ${TMPDIR}/bash-zzz-$$ << EOF
abc
EOF
cat >> ${TMPDIR}/bash-zzz-$$ << EOF
def ghi
jkl mno
EOF
cat ${TMPDIR}/bash-zzz-$$
rm -f ${TMPDIR}/bash-zzz-$$

# check behavior of double quotes and backslashes in here-documents
cat <<EOF
echo "
EOF

cat <<EOF
echo \"
EOF


echo $(
	cat <<< "comsub here-string"
)
