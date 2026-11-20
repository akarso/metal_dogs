package catalog

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadPrices_AmountStrings(t *testing.T) {
	cases := []struct {
		amount string
		minor  int64
	}{
		{amount: "80,49", minor: 8049},
		{amount: "80.49", minor: 8049},
		{amount: "1.234,56", minor: 123456},
		{amount: "1,234.56", minor: 123456},
		{amount: "80,499"},
		{amount: "80 zł"},
		{amount: "80 PLN"},
		{amount: "-5"},
		{amount: "0"},
		{amount: ""},
		{amount: "abc"},
		{amount: "1,234"},
		{amount: "1."},
		{amount: ".50"},
		{amount: "1.234.567"},
		{amount: "1234.567,89"},
	}
	for _, tc := range cases {
		t.Run(tc.amount, func(t *testing.T) {
			rows, err := loadPrices(writePriceCSV(t, tc.amount))
			if tc.minor == 0 {
				if err == nil {
					t.Fatal("expected error")
				}
				msg := err.Error()
				quoted := tc.amount
				if tc.amount == "" {
					quoted = `invalid amount ""`
				}
				for _, part := range []string{"prices.csv", "row 2", "column amount", quoted} {
					if !strings.Contains(msg, part) {
						t.Fatalf("error %q missing %q", msg, part)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 || rows[0].minor != tc.minor || rows[0].currency != "PLN" {
				t.Fatalf("rows = %+v", rows)
			}
		})
	}
}

func writePriceCSV(t *testing.T, amount string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "prices.csv")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.WriteAll([][]string{
		{"sku", "currency", "amount"},
		{"MDOGS-TEE-S", "pln", amount},
	}); err != nil {
		t.Fatal(err)
	}
	return path
}
