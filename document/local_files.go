package document

// Implements document.Set
type LocalFiles struct {
	WorkDir	string
}

func (l *LocalFiles) Get() (err error){
	// nothing to do here, they are already on the file system
	return nil
}

// Return the path to the documents
func (l *LocalFiles) Path() (path string){
	return l.WorkDir
}

func (l *LocalFiles) CleanUp() {
	// NOOP, should probably not remove files that existed before execution
	return
}
