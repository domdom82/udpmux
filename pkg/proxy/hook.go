package proxy

type Hook func(*ClientSession, []byte) ([]byte, error)
