// Generated from data/execution-compatibility.json. Do not edit.
package control

const MatrixVersion = 1
const MatrixHash = "c2963879b58386fdf99df913edda790ff147f01290cedb476b5216990ea6b795"

var controlAPIs = []int{1, 2, 3, 4}
var profiles = []Profile{
	{ID: "legacy", Input: 0, Result: 0, Observation: 0, ControlAPI: 1, Description: 1, Job: 0, Progress: 0, Event: 0},
	{ID: "input", Input: 1, Result: 0, Observation: 0, ControlAPI: 2, Description: 2, Job: 1, Progress: 0, Event: 0},
	{ID: "result", Input: 1, Result: 1, Observation: 0, ControlAPI: 3, Description: 3, Job: 2, Progress: 0, Event: 0},
	{ID: "observations", Input: 1, Result: 1, Observation: 1, ControlAPI: 4, Description: 4, Job: 3, Progress: 2, Event: 2},
}
