package sqlutil

import "testing"

func TestQuoting(t *testing.T) {
	for _, tc := range []struct{ got, want string }{
		{QuoteIdent("plain"), `"plain"`},
		{QuoteIdent(`a"b`), `"a""b"`},
		{QuoteIdent(`"; DROP TABLE t; --`), `"""; DROP TABLE t; --"`},
		{QuoteLiteral("plain"), `'plain'`},
		{QuoteLiteral("o'ne"), `'o''ne'`},
		{QuoteLiteral("'); DROP TABLE t; --"), `'''); DROP TABLE t; --'`},
		{EscapeLiteral("a'b''c"), `a''b''''c`},
	} {
		if tc.got != tc.want {
			t.Errorf("got %s, want %s", tc.got, tc.want)
		}
	}
}
