// Generated from data/execution-compatibility.json. Do not edit.
export const EXECUTION_MATRIX={
  "matrixVersion": 1,
  "controlAPIs": [
    1,
    2,
    3,
    4
  ],
  "profiles": [
    {
      "id": "legacy",
      "input": 0,
      "result": 0,
      "observation": 0,
      "controlAPI": 1,
      "description": 1,
      "job": 0,
      "progress": 0,
      "event": 0
    },
    {
      "id": "input",
      "input": 1,
      "result": 0,
      "observation": 0,
      "controlAPI": 2,
      "description": 2,
      "job": 1,
      "progress": 0,
      "event": 0
    },
    {
      "id": "result",
      "input": 1,
      "result": 1,
      "observation": 0,
      "controlAPI": 3,
      "description": 3,
      "job": 2,
      "progress": 0,
      "event": 0
    },
    {
      "id": "observations",
      "input": 1,
      "result": 1,
      "observation": 1,
      "controlAPI": 4,
      "description": 4,
      "job": 3,
      "progress": 2,
      "event": 2
    },
    {
      "id": "scan_input",
      "input": 2,
      "result": 1,
      "observation": 1,
      "controlAPI": 4,
      "description": 5,
      "job": 4,
      "progress": 2,
      "event": 2
    },
    {
      "id": "sdk_operation",
      "input": 3,
      "result": 1,
      "observation": 1,
      "controlAPI": 4,
      "description": 6,
      "job": 5,
      "progress": 2,
      "event": 2
    },
    {
      "id": "git_diff",
      "input": 4,
      "result": 1,
      "observation": 1,
      "controlAPI": 4,
      "description": 7,
      "job": 6,
      "progress": 2,
      "event": 2
    },
    {
      "id": "component_planning",
      "input": 5,
      "result": 1,
      "observation": 1,
      "controlAPI": 4,
      "description": 8,
      "job": 7,
      "progress": 2,
      "event": 2
    },
    {
      "id": "component_scan",
      "input": 6,
      "result": 1,
      "observation": 1,
      "controlAPI": 4,
      "description": 9,
      "job": 8,
      "progress": 2,
      "event": 2
    }
  ],
  "policies": {
    "discovery": "presence-only",
    "fallbackOn": [
      404,
      405
    ],
    "claimFallback": false,
    "unknownContracts": "reject-before-model",
    "history": "preserve-readers",
    "upgradeOrder": [
      "server",
      "agent-and-scanner",
      "web"
    ]
  }
} as const;
export const EXECUTION_MATRIX_HASH="7f77589f40907b7d591be79f1a883d4d5f1f004f2a304db3c5cbde705d9cbfb9" as const;
