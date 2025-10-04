package secondpackage

import (
	"fmt"
	"testing"

	"github.com/rprtr258/punused-testdata/firstpackage"
)

func TestUseTestlib1(t *testing.T) {
	fmt.Println(firstpackage.OnlyUsedInTestConst)
}
