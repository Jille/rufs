var nodes, edges;

function init() {
    createGraph();

    reloadData();
    setInterval(reloadData, 1000);
}

function createGraph() {
    nodes = new vis.DataSet([]);
    edges = new vis.DataSet([]);

    // create a network
    var container = document.getElementById("graph");
    var data = {
        nodes: nodes,
        edges: edges,
    };
    var options = {
        nodes: {
        },
        edges: {
            arrows: {
                to: {
                    enabled: true,
                },
            },
        },
        interaction: {
            zoomView: false,
        },
        physics: {
            barnesHut: {
                gravitationalConstant: -15000,
            },
        },
    };
    network = new vis.Network(container, data, options);
}

function reloadData() {
    fetch("/visualize/api/received_bytes")
        .then(response => response.json())
        .then(data => updateGraph(data))
        .catch(err => console.error(err))
}

function updateGraph(data) {
    console.log('Updating graph', data)

    nodes.update(data.nodes.map(({ name }) => {
        return { id: name, label: name }
    }))
    edges.update(data.edges.map(({ sender, receiver, mode, bytes_per_second }) => {
        return {
            id: `${sender}->${receiver}:${mode}`,
            from: sender,
            to: receiver,
            label: `${Math.round(bytes_per_second / 1024)} KB/s`,
            value: bytes_per_second,
        }
    }))
}
