package stackerr

import (
	"github.com/Hoverhuang-er/overlord/pkg/log"
	"github.com/fatih/color"
	"github.com/pkg/errors"
)

func ReplaceErrStack(err error) error {
	log.Errorf(color.RedString("OUT[ERRO]: %+s", err.Error()))
	return errors.WithStack(err)
}
