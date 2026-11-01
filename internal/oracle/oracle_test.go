//go:build oracle

package oracle

import "testing"

func TestOracle(t *testing.T) {
	j, _, ok := Decode("B6", "/BOMCAYA.ADS.VT-ANQ", Downlink)
	t.Log(ok, j)
}
