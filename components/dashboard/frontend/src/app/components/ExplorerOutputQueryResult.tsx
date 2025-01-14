import * as React from 'react';
import Container from "react-bootstrap/Container";
import ListGroup from "react-bootstrap/ListGroup";
import NodeStore from "../stores/NodeStore";
import { inject, observer } from "mobx-react";
import ExplorerStore from "../stores/ExplorerStore";
import Badge from "react-bootstrap/Badge";
import {Link} from 'react-router-dom';
import {displayManaUnit} from "../utils";
import {resolveBase58ConflictID} from "../utils/conflict";
import {outputToComponent} from "../utils/output";
import {resolveConfirmationState} from "../utils/confirmation_state";

interface Props {
    nodeStore?: NodeStore;
    explorerStore?: ExplorerStore;
    match?: {
        params: {
            id: string,
        }
    }
}

@inject("nodeStore")
@inject("explorerStore")
@observer
export class ExplorerOutputQueryResult extends React.Component<Props, any> {
    componentDidMount() {
        this.props.explorerStore.getOutput(this.props.match.params.id);
        this.props.explorerStore.getPendingMana(this.props.match.params.id);
        this.props.explorerStore.getOutputMetadata(this.props.match.params.id);
        this.props.explorerStore.getOutputConsumers(this.props.match.params.id);
    }

    componentWillUnmount() {
        this.props.explorerStore.reset();
    }
    render() {
        let {id} = this.props.match.params;
        let { query_err, output, pendingMana, outputMetadata, outputConsumers } = this.props.explorerStore;

        let renderTriBool = (val: string) => {
            if (val === "true"){
                return <Badge variant={"success"}>True</Badge>
            }
            if (val === "false"){
                return <Badge variant={"danger"}>False</Badge>
            }
            return <Badge variant={"warning"}>Maybe</Badge>
        }

        if (query_err) {
            return (
                <Container>
                    <h4>Output not found - 404</h4>
                    <span>{id}</span>
                </Container>
            );
        }
        return (
            <Container>
                <h4>Output</h4>
                {output && <div className={"mb-2"}>
                    {outputToComponent(output)}
                    <ListGroup>
                        {pendingMana && <ListGroup.Item>
                            Pending Mana
                            <hr/>
                            <div>Value: {displayManaUnit(pendingMana.mana)}</div>
                            <div>Timestamp: {new Date(pendingMana.timestamp * 1000).toLocaleString()}</div>
                        </ListGroup.Item>}
                    </ListGroup>
                </div>}

                <h4>Metadata</h4>
                {outputMetadata && <div className={"mb-2"}>
                    <ListGroup>
                        <ListGroup.Item>Transaction ID: <a href={`/explorer/transaction/${outputMetadata.outputID.transactionID}`}>{outputMetadata.outputID.transactionID}</a> </ListGroup.Item>
                        ConflictIDs: 
                        <ListGroup>
                            {
                                outputMetadata.conflictIDs.map((value, index) => {
                                    return (
                                        <ListGroup.Item key={"ConflictID" + index + 1} className="text-break">
                                            <Link to={`/explorer/conflict/${value}`}>
                                                {resolveBase58ConflictID(value)}
                                            </Link>
                                        </ListGroup.Item>
                                    )
                                })
                            }
                        </ListGroup>
                        <ListGroup.Item>Consumer Count: {outputMetadata.consumerCount}</ListGroup.Item>
                        <ListGroup.Item>Confirmed Consumer: <a href={`/explorer/transaction/${outputMetadata.confirmedConsumer}`}>{outputMetadata.confirmedConsumer}</a> </ListGroup.Item>
                        <ListGroup.Item>Confirmation State: {resolveConfirmationState(outputMetadata.confirmationState)}</ListGroup.Item>
                        <ListGroup.Item>Confirmation State Time: {new Date(outputMetadata.confirmationStateTime * 1000).toLocaleString()}</ListGroup.Item>
                    </ListGroup>
                </div>}

                <h4>Consumers</h4>
                {outputConsumers && <div>
                    <ListGroup>
                        {outputConsumers.consumers.map((c,i) => <ListGroup.Item key={i}>
                            <div>Transaction ID:  <a href={`/explorer/transaction/${c.transactionID}`}>{c.transactionID}</a></div>
                            <div>Valid: {renderTriBool(c.valid)} </div>
                        </ListGroup.Item>)}
                    </ListGroup>
                </div>}
            </Container>
        )
    }
}
