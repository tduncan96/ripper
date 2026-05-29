package preflight

type Gate struct {
	RipConfig  error
	LibrConfig error
}

var MasterGate Gate

func Init() error {
	ripErr, librErr, err := ReadConfigFiles()
	if err != nil {
		return err
	}
	MasterGate.RipConfig = ripErr
	MasterGate.LibrConfig = librErr

	return nil
}
