import FileIcon from '@sourcegraph/icons/lib/File'
import FolderIcon from '@sourcegraph/icons/lib/Folder'
import formatDistance from 'date-fns/formatDistance'
import * as React from 'react'
import { Link } from 'react-router-dom'
import VisibilitySensor from 'react-visibility-sensor'
import 'rxjs/add/operator/catch'
import 'rxjs/add/operator/filter'
import 'rxjs/add/operator/switchMap'
import { Subject } from 'rxjs/Subject'
import { Subscription } from 'rxjs/Subscription'
import { UserAvatar } from '../settings/user/UserAvatar'
import { parseCommitDateString } from '../util/time'
import { toBlobURL, toTreeURL } from '../util/url'
import { fetchFileCommitInfo } from './backend'

interface Props {
    isDirectory: boolean
    repoPath: string
    filePath: string
    commitID: string
    rev?: string
}

interface State {
    commitInfo?: GQL.ICommitInfo
}

export class DirectoryPageEntry extends React.PureComponent<Props, State> {
    public state: State = {}
    private isVisible = false
    private componentUpdates = new Subject<Props>()
    private subscriptions = new Subscription()

    constructor(props: Props) {
        super(props)
        this.subscriptions.add(
            this.componentUpdates
                .filter(() => this.isVisible)
                .switchMap(props =>
                    fetchFileCommitInfo(props).catch(err => {
                        console.error(err)
                        return []
                    })
                )
                .subscribe(
                    commitInfo => {
                        this.setState({ commitInfo })
                    },
                    err => console.error(err)
                )
        )
    }

    public onChangeVisibility = (isVisible: boolean): void => {
        this.isVisible = isVisible
        this.componentUpdates.next(this.props)
    }

    public componentWillReceiveProps(newProps: Props): void {
        this.componentUpdates.next(newProps)
    }

    public componentWillUnmount(): void {
        this.subscriptions.unsubscribe()
    }

    public render(): JSX.Element | null {
        const lastCommit = this.state.commitInfo
        const person = lastCommit && lastCommit.committer && lastCommit.committer.person && lastCommit.committer.person
        const date =
            lastCommit &&
            lastCommit.committer &&
            formatDistance(parseCommitDateString(lastCommit.committer.date), new Date(), { addSuffix: true })

        return (
            <VisibilitySensor onChange={this.onChangeVisibility} partialVisibility={true}>
                <tr className="dir-page-entry__row">
                    <td
                        className="dir-page-entry__name-cell"
                        colSpan={2}
                        title={this.getLastPathPart(this.props.filePath)}
                    >
                        <span className="truncated">
                            {this.props.isDirectory ? (
                                <FolderIcon className="icon-inline" />
                            ) : (
                                <FileIcon className="icon-inline" />
                            )}
                            {this.props.isDirectory ? (
                                <Link
                                    to={toTreeURL({
                                        repoPath: this.props.repoPath,
                                        filePath: this.props.filePath,
                                        rev: this.props.rev,
                                    })}
                                    className={'name-link'}
                                >
                                    {this.getLastPathPart(this.props.filePath)}
                                </Link>
                            ) : (
                                <Link
                                    to={toBlobURL({
                                        repoPath: this.props.repoPath,
                                        filePath: this.props.filePath,
                                        rev: this.props.rev,
                                    })}
                                    className={'name-link'}
                                >
                                    {this.getLastPathPart(this.props.filePath)}
                                </Link>
                            )}
                        </span>
                    </td>
                    <td className="dir-page-entry-entry__commit-message-cell" title={lastCommit && lastCommit.message}>
                        {lastCommit && lastCommit.message}
                    </td>
                    <td className="dir-page-entry__committer-cell" title={person ? person.name : undefined}>
                        {person && <UserAvatar user={person} />}
                        {person && person.name}
                    </td>
                    <td className="dir-page-entry__date-cell" title={date ? date : undefined}>
                        {date}
                    </td>
                    <td
                        className="dir-page-entry__commit-hash-cell"
                        title={lastCommit && lastCommit.rev.substring(0, 7)}
                    >
                        <Link
                            to={toBlobURL({
                                repoPath: this.props.repoPath,
                                filePath: this.props.filePath,
                                rev: lastCommit && lastCommit.rev,
                            })}
                        >
                            {lastCommit && lastCommit.rev.substring(0, 7)}
                        </Link>
                    </td>
                </tr>
            </VisibilitySensor>
        )
    }

    private getLastPathPart(filePath: string): string {
        return filePath.substr(filePath.lastIndexOf('/') + 1)
    }
}
