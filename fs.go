package wasmexec

// reference:
// https://github.com/hack-pad/hackpad/blob/1f6b4afdb875e099505f5c3ed65751bce9eecce0/internal/js/fs/fs.go#L29

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
)

// HostFS describes an instance that has implemented the FS methods.
type HostFS interface {
	fs.FS
	fs.StatFS
	// fs.ReadDirFS

	// Chmod changes the mode of a file.
	Chmod(name string, mode fs.FileMode) error
}

// fsErrorResponse unwraps a PathError response.
func fsErrorResponse(err error) []any {
	if os.IsNotExist(err) {
		return errorResponse(eNOENT)
	} else if baseErr := errors.Unwrap(err); baseErr != nil {
		err = baseErr
	}
	errnoErr, _ := err.(syscall.Errno)
	if errnoErr != 0 {
		return errorResponse(errno(errnoErr.Error()))
	} else {
		return []any{jsProperties{"error": string(err.Error())}}
	}
}

// modeBitTranslation translates from fs.FileMode to the JS equivalent.
var modeBitTranslation = map[fs.FileMode]uint32{
	fs.ModeDir:        syscall.S_IFDIR,
	fs.ModeCharDevice: syscall.S_IFCHR,
	fs.ModeNamedPipe:  syscall.S_IFIFO,
	fs.ModeSymlink:    syscall.S_IFLNK,
	fs.ModeSocket:     syscall.S_IFSOCK,
}

func jsMode(mode fs.FileMode) uint32 {
	for goBit, jsBit := range modeBitTranslation {
		if mode&goBit == goBit {
			mode = mode & ^goBit | fs.FileMode(jsBit)
		}
	}
	return uint32(mode)
}

func blockCount(size, blockSize int64) int64 {
	blocks := size / blockSize
	if size%blockSize > 0 {
		return blocks + 1
	}
	return blocks
}

var (
	funcTrue = newjsFunction(func(args []any) any {
		return true
	})
	funcFalse = newjsFunction(func(args []any) any {
		return false
	})
)

func jsBoolFunc(b bool) *jsFunction {
	if b {
		return funcTrue
	}
	return funcFalse
}

// jsStat converts the FileInfo into the equivalent JS object.
func jsStat(info fs.FileInfo) *jsObject {
	if info == nil {
		return nil
	}
	const blockSize = 4096 // TODO find useful value for blksize
	modTime := info.ModTime().UnixNano() / 1e6
	return &jsObject{
		properties: jsProperties{
			"dev":     0,
			"ino":     0,
			"mode":    jsMode(info.Mode()),
			"nlink":   1,
			"uid":     0, // TODO use real values for uid and gid
			"gid":     0,
			"rdev":    0,
			"size":    info.Size(),
			"blksize": blockSize,
			"blocks":  blockCount(info.Size(), blockSize),
			"atimeMs": modTime,
			"mtimeMs": modTime,
			"ctimeMs": modTime,

			"isBlockDevice":     funcFalse,
			"isCharacterDevice": funcFalse,
			"isDirectory":       jsBoolFunc(info.IsDir()),
			"isFIFO":            funcFalse,
			"isFile":            jsBoolFunc(info.Mode().IsRegular()),
			"isSocket":          funcFalse,
			"isSymbolicLink":    jsBoolFunc(info.Mode()&fs.ModeSymlink == fs.ModeSymlink),
		},
	}
}

// fsChmod implements the chmod syscall callback
// chmod(path, mode, callback)
func fsChmod(mod *Module, hostFS HostFS) *jsFunction {
	if hostFS == nil {
		return errorCallback(eNOSYS)
	}
	return &jsFunction{
		fn: func(args []any) any {
			if len(args) != 3 {
				mod.error("fs.chmod: %d: invalid number of arguments", len(args))
				return nil
			}

			fpath, ok := args[0].(*jsString)
			if !ok {
				mod.error("fs.chmod: %T: not type string", args[0])
				return nil
			}

			mode, ok := args[1].(int)
			if !ok {
				mod.error("fs.chmod: %T: not type int", args[1])
				return nil
			}

			callback, ok := args[2].(*jsFunction)
			if !ok {
				mod.error("fs.chmod: %T: not type jsFunction", args[2])
				return nil
			}

			err := hostFS.Chmod(fpath.data, fs.FileMode(mode))
			if err != nil {
				mod.error("fs.chmod: %v", err)
				return fsErrorResponse(err)
			}

			callback.fn([]any{nil})
			return nil
		},
	}
}

// fsStat implements the stat syscall callback
// stat(path, callback)
func fsStat(mod *Module, hostFS HostFS) *jsFunction {
	if hostFS == nil {
		return errorCallback(eNOSYS)
	}
	return &jsFunction{
		fn: func(args []any) any {
			if len(args) != 2 {
				mod.error("fs.stat: %d: invalid number of arguments", len(args))
				return nil
			}

			fpath, ok := args[0].(*jsString)
			if !ok {
				mod.error("fs.stat: %T: not type string", args[0])
				return nil
			}

			callback, ok := args[1].(*jsFunction)
			if !ok {
				mod.error("fs.chmod: %T: not type jsFunction", args[1])
				return nil
			}

			fi, err := hostFS.Stat(fpath.data)
			if err != nil {
				// mod.error("fs.stat: %s", err.Error())
				return fsErrorResponse(err)
			}

			callback.fn([]any{jsStat(fi)})
			return nil
		},
	}
}
