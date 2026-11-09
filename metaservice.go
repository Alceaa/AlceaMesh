package main

type MetaService struct{}

func (m *MetaService) Version() string {
	return appVersion
}

func (m *MetaService) Coordinator() string {
	return coordinatorURL()
}
