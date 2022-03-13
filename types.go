package main

type Defect struct {
	markid string
	num    int
}

type DefectDetail struct {
	seq_id   string
	markid   string
	markdate string
	marktime string
	gps_y    string
	gps_x    string
	address  string
	photo    string
}

type Roadmark struct {
	markid string
	name   string
}
