// Generated from data/execution-compatibility.json. Do not edit.
package control

const MatrixVersion = 1
const MatrixHash = "f2947398419ab74a0f7463deb00be0127af4dd72c40ff02579cbebfdaa2a653c"

var controlAPIs = []int{1, 2, 3, 4}
var profiles = []Profile{
	{ID: "legacy", Input: 0, Result: 0, Observation: 0, ControlAPI: 1, Description: 1, Job: 0, Progress: 0, Event: 0},
	{ID: "input", Input: 1, Result: 0, Observation: 0, ControlAPI: 2, Description: 2, Job: 1, Progress: 0, Event: 0},
	{ID: "result", Input: 1, Result: 1, Observation: 0, ControlAPI: 3, Description: 3, Job: 2, Progress: 0, Event: 0},
	{ID: "observations", Input: 1, Result: 1, Observation: 1, ControlAPI: 4, Description: 4, Job: 3, Progress: 2, Event: 2},
	{ID: "scan_input", Input: 2, Result: 1, Observation: 1, ControlAPI: 4, Description: 5, Job: 4, Progress: 2, Event: 2},
	{ID: "sdk_operation", Input: 3, Result: 1, Observation: 1, ControlAPI: 4, Description: 6, Job: 5, Progress: 2, Event: 2},
	{ID: "git_diff", Input: 4, Result: 1, Observation: 1, ControlAPI: 4, Description: 7, Job: 6, Progress: 2, Event: 2},
	{ID: "component_planning", Input: 5, Result: 1, Observation: 1, ControlAPI: 4, Description: 8, Job: 7, Progress: 2, Event: 2},
	{ID: "component_scan", Input: 6, Result: 1, Observation: 1, ControlAPI: 4, Description: 9, Job: 8, Progress: 2, Event: 2},
}
